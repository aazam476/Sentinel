# 0043 — A recent second factor before a script reaches more than one machine

**Date:** 2026-11-14 · **Status:** Accepted

## Context

SEC-07 asks for a second-factor check within the last 15 minutes before a script runs on more
than one machine, as for opening a terminal. The Scripts page (ADR 0024) was built without it:
a session whose last check was hours ago could run a script as root on every machine at once.
The security review listed the check as done, and no test named it.

SEC-07 names one action, running a script. Two more things a person can do end with a script
running on several machines, and without them the check would be a formality: saving a
schedule, which runs the script later with no one there, and changing the text of a script
that a schedule already runs.

## Decision

A person needs the recent check for exactly the actions that can put a script on more than
one machine:

- **Running a script now**, when its target is more than one machine today. A tag that only
  one machine has counts as one machine, as does a target naming one machine twice.
- **Saving a schedule that is on and can reach more than one machine**: a tag, whatever it
  matches today, since machines are tagged later without anyone opening the schedule again;
  or several machines. Saving a paused schedule needs no check, and pausing one never does,
  so a schedule can always be stopped quickly. Switching one on does.
- **Changing a script that such a schedule runs.** The schedule would run the new text on
  those machines at its next time.

One machine needs no check, in each of the three, as SEC-07 says. The schedule itself runs
without anyone's check: the person who saved it gave theirs then.

The scripts service asks `auth.Service` through a small `StepUp` interface, as the settings
service does (ADR 0041). A server assembled without one refuses these actions rather than
skip the check. The web UI shows the usual "Confirm it’s you" dialog and repeats the action
once the check passes.

## Consequences

- An operator who edits a script on a fleet-wide schedule is asked for their second factor
  if they haven't given it in the last 15 minutes, even for a small change.
- Refused attempts aren't audited, like other step-up refusals: nothing happened. The run,
  the schedule and the script change are audited once they go through, as before.
- Adding a tag to more machines widens what a tag's schedule reaches without a new check.
  Tags are an admin's to change, and the script that runs is the one that was checked.
- A script on one machine still needs no check, although it is as powerful there as a
  terminal, which needs one. That is SEC-07 as written; widening it is a specification
  change, listed for the maintainer.
