// Package update implements the agent's self-update (task 8.4): checking
// GET /print-agents/version at startup and every 6h (design.md D8),
// downloading a newer signed build, verifying its sha256 and Authenticode
// signature, and swapping it into place before relaunching. It never
// updates while a print job is in flight (agent.Runner.Busy) and any
// failure is logged and leaves the current version running untouched.
package update
