// Package singleinstance prevents two copies of the agent from running on
// the same machine at once (task 8.4): a named OS mutex arbitrates which
// process is "the" instance, and a small handoff file in the config
// directory lets a second launch (e.g. double-clicking the exe again, or
// the Task Scheduler entry firing while a manual run is already up) open
// the running instance's status page instead of starting a second agent —
// duplicate agents on one PC would duplicate every print.
package singleinstance
