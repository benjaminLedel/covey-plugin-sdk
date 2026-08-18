// The contract between Covey and a target-system plugin. Deliberately its own
// module and deliberately dependency-free: a plugin author depends on this, not
// on Covey's whole tree.
module github.com/benjaminLedel/covey-plugin-sdk

go 1.24

// The go directive above is what a PLUGIN AUTHOR needs — deliberately low, so
// this contract does not drag anybody onto a toolchain they have no other
// reason for. The toolchain line below applies only where this module is the
// main one (its own CI, its own tests) and pins the build to a standard library
// without known holes: govulncheck is a hard step here, and it reports the
// stdlib of whatever toolchain built the code. Raising it is the fix when it
// reports one; raising the go directive would not be.
toolchain go1.26.6
