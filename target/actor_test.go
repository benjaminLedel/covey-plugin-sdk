package target

import (
	"context"
	"testing"
)

func TestActorRoundTrip(t *testing.T) {
	if _, ok := ActorFrom(context.Background()); ok {
		t.Fatal("no actor expected on a bare context")
	}
	ctx := WithActor(context.Background(), Actor{Slug: "dev", Name: "Developer"})
	a, ok := ActorFrom(ctx)
	if !ok || a.Slug != "dev" || a.Name != "Developer" {
		t.Fatalf("got %+v %v", a, ok)
	}
	if _, ok := ActorFrom(WithActor(context.Background(), Actor{Name: "no slug"})); ok {
		t.Fatal("an actor without a slug must not count")
	}
}
