// Package application contains transport-independent primitives shared by
// ATM's application services.
//
// Command-line, IPC, controller, and hook adapters should translate their own
// inputs into these types before invoking a use case. This package deliberately
// has no dependency on Cobra, persistence, or any ATM domain package, so those
// adapters and services can share call identity and error semantics without
// becoming coupled to one another.
package application
