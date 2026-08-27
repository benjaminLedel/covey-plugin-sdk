package target

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Checkouts used to pile up without bound in the persistent home — a QA agent
// ran its 40 GB overlay full that way and one of its runs was killed. The
// newest working copies survive, the rest fall away, and the one just created
// survives under all circumstances.
func TestPruneOldCheckoutsKeepsTheNewestAndTheCurrent(t *testing.T) {
	t.Setenv("COVEY_CHECKOUT_KEEP", "3")
	home := t.TempDir()
	repos := ReposDir(home)
	now := time.Now()
	// Six working copies, p0 the oldest … p5 the newest.
	for i := 0; i < 6; i++ {
		d := filepath.Join(repos, "p"+string(rune('0'+i)))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(d, now, now.Add(-time.Duration(6-i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	// The current checkout is the OLDEST by mtime — it must survive anyway.
	current := filepath.Join(repos, "p0")
	removed := PruneOldCheckouts(home, current)
	if len(removed) != 3 {
		t.Fatalf("expected 3 removals, got %v", removed)
	}
	for _, name := range []string{"p0", "p5", "p4"} {
		if _, err := os.Stat(filepath.Join(repos, name)); err != nil {
			t.Fatalf("%s must survive: %v", name, err)
		}
	}
	for _, name := range []string{"p1", "p2", "p3"} {
		if _, err := os.Stat(filepath.Join(repos, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should be gone, err=%v", name, err)
		}
	}
	if note := CheckoutPruneNote(removed); note == "" {
		t.Fatal("the agent has to learn that its working copy is gone")
	}
}

// Switched off (0) and below the limit nothing is touched.
func TestPruneOldCheckoutsRestraint(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ReposDir(home), "p1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := PruneOldCheckouts(home, ""); got != nil {
		t.Fatalf("below the limit nothing is removed: %v", got)
	}
	t.Setenv("COVEY_CHECKOUT_KEEP", "0")
	if got := PruneOldCheckouts(home, ""); got != nil {
		t.Fatalf("switched off nothing is removed: %v", got)
	}
	if CheckoutPruneNote(nil) != "" {
		t.Fatal("no removal, no sentence")
	}
}

/* Fünf beiläufige Abrufe haben auf einer produktiven Instanz einen 700-MB-Baum
   verdrängt, in dem ein Agent seit Stunden arbeitete — zusammengesetzt aus
   dreizehn Teil-Abrufen. „Am längsten nicht benutzt" ist die richtige Regel für
   Kopien, die man liest; ein Checkout ist der Ort, an dem gearbeitet wird
   (covey-plugin-sdk#1). */

func TestEineFrischBenutzteKopieWirdNichtVerdraengt(t *testing.T) {
	t.Setenv("COVEY_CHECKOUT_KEEP", "3")
	home := t.TempDir()
	repos := ReposDir(home)
	jetzt := time.Now()

	// Die Lage aus dem Vorfall: der Arbeitsbaum wurde vor zehn Minuten zuletzt
	// beschrieben, danach holte derselbe Lauf in einem Zug fünf fremde
	// Projekte. Nach Rang ist der Arbeitsbaum damit der sechste von sechs —
	// benutzt wurde er trotzdem, und zwar in diesem Lauf.
	arbeit := filepath.Join(repos, "arbeitsbaum")
	if err := os.MkdirAll(arbeit, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(arbeit, jetzt, jetzt.Add(-10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		d := filepath.Join(repos, "fremd"+string(rune('0'+i)))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		frisch := jetzt.Add(-time.Duration(i+1) * time.Minute)
		if err := os.Chtimes(d, frisch, frisch); err != nil {
			t.Fatal(err)
		}
	}

	// Der aktuelle Abruf ist einer der fremden — der Arbeitsbaum steht also
	// nicht unter dem Schutz von `keep`, sondern nur unter dem der Schonfrist.
	removed := PruneOldCheckouts(home, filepath.Join(repos, "fremd0"))
	if _, err := os.Stat(arbeit); err != nil {
		t.Fatalf("der Baum, in dem gearbeitet wird, wurde verdrängt (entfernt: %v)", removed)
	}
	if len(removed) != 0 {
		t.Fatalf("innerhalb der Frist darf nichts verdrängt werden, entfernt: %v", removed)
	}

	// Und ohne Frist wäre er weg — das ist der Unterschied, um den es geht.
	t.Setenv("COVEY_CHECKOUT_PROTECT_MINS", "0")
	removed = PruneOldCheckouts(home, filepath.Join(repos, "fremd0"))
	if _, err := os.Stat(arbeit); err == nil {
		t.Fatalf("ohne Frist müsste die Rangfolge ihn treffen (entfernt: %v)", removed)
	}
}

// Abschaltbar, und dann gilt wieder die reine Rangfolge. Wer den Platz
// braucht, soll ihn bekommen können.
func TestDieSchonfristLaesstSichAbschalten(t *testing.T) {
	t.Setenv("COVEY_CHECKOUT_KEEP", "2")
	t.Setenv("COVEY_CHECKOUT_PROTECT_MINS", "0")
	home := t.TempDir()
	repos := ReposDir(home)
	jetzt := time.Now()
	for i := 0; i < 4; i++ {
		d := filepath.Join(repos, "p"+string(rune('0'+i)))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		// Alle vier eben erst benutzt — mit Frist bliebe alles stehen.
		if err := os.Chtimes(d, jetzt, jetzt.Add(-time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if removed := PruneOldCheckouts(home, filepath.Join(repos, "p0")); len(removed) == 0 {
		t.Fatal("ohne Frist muss die Rangfolge wieder greifen")
	}
}

// Und mit Frist bleibt in derselben Lage alles stehen — das ist die
// Gegenprobe zum Test darüber.
func TestMitFristBleibtFrischBenutztesStehen(t *testing.T) {
	t.Setenv("COVEY_CHECKOUT_KEEP", "2")
	home := t.TempDir()
	repos := ReposDir(home)
	jetzt := time.Now()
	for i := 0; i < 4; i++ {
		d := filepath.Join(repos, "p"+string(rune('0'+i)))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(d, jetzt, jetzt.Add(-time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if removed := PruneOldCheckouts(home, filepath.Join(repos, "p0")); len(removed) != 0 {
		t.Fatalf("frisch benutzte Kopien wurden verdrängt: %v", removed)
	}
}
