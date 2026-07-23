# Scheduling: where it stands, what could be better

Researched 2026-07-19 against the successive-relearning literature and the
FSRS ecosystem. This is the maintained list of candidate scheduler changes,
roughly ordered by value for effort. The settled refusals at the bottom are
not candidates and should not be re-proposed.

## Current state

Two layers, both in good shape by the literature:

- **Between sessions** (`progress.Schedule`): a fixed review ladder of
  1, 3, 7, 14, 30, 60, 120 days. A clean session advances one rung, a
  session with a lapse halves the level. This is successive relearning
  (Rawson & Dunlosky) with geometric backoff on a miss.
- **Within a session** (`quiz.Engine`): criterion learning. New cards owe 3
  spaced recalls, reviews owe 1, a miss raises the debt to 2. Repetitions
  are spaced by intervening serves (3 after a miss, 8 after a correct, 5
  for a confused-with card). New cards enter through a 4-slot window.

The session layer matches the research closely. Three recalls for new
material and one per relearning session is exactly the published
recommendation, and later work confirms that pushing the initial criterion
above 3 is overlearning that spaced relearning washes out anyway. The
within-session gaps follow Karpicke & Bauernschmidt: any nonzero gap beats
back-to-back repetition, the exact pattern matters little.

The weak layer is the between-session ladder. It is position-based (blind
to how long a card actually survived), deterministic (cards learned
together stay in lockstep forever), and clock-based rather than
day-based. Every item below except 7 and 9 targets it.

## Candidate improvements

### 1. Credit the real interval on an overdue success

The ladder advances by rung position, not demonstrated retention. A card
at rung 2 (3 days) answered correctly 40 days late just proved 43 days of
retention, yet it advances to 7 days. FSRS gets this right by design: the
lower the predicted recall at a successful review, the larger the
stability gain. The ladder-native version is cheap. On a clean success,
advance to at least the first rung covering the elapsed time, so next
interval = max(ladder step, first rung >= days actually survived). This is
the biggest practical win for irregular studying and backlogs.

The early-review side stays as is. Holding the rung on a clean ahead
completion is the honest binary approximation, and partial credit for
early reviews needs a real memory model (item 8) to do properly.

**Landed 2026-07-23** in `progress.Schedule`: a clean success advances to
at least the first rung covering scheduled + overdue days, whole days
only. Lapses and early reviews are unchanged.

### 2. Interval fuzz, then load balancing

Deterministic intervals keep same-day cohorts in lockstep: identical due
dates forever, spiky review days, and the deck returning in a
recognizable order (an answer-priming risk on top of a workload problem).
Anki applies graduated fuzz (3 days draws from 2 to 4, 7 from 5 to 9, 90
from 86 to 94) and its load balancer picks the least-loaded day inside
the fuzz range. Fuzz alone is a few lines in `Schedule`. Balancing needs
a due-count-per-day query on the store and is optional on top.

### 3. Day-anchored due dates

`Due = now + days * 24h` means a card studied at 21:00 is not due at
20:00 the next evening, so study time ratchets later and "due today" is
inconsistent with the same-day gate (which compares calendar days).
Anchor the due time to the start of the local day after adding the
interval, the way Anki schedules whole days with a rollover hour. Small
correctness fix, best done together with item 2 since both touch
`Schedule`.

### 4. Relative overdueness ordering

`SplitDue` serves the most overdue card first in absolute days. A card 5
days late on a 3-day interval is in far more danger than one 10 days late
on a 120-day interval. Sort by overdue-days divided by interval instead.
Zero cost with current data (Level gives the interval). The Anki
ecosystem's simulations went further and made descending retrievability
the default ordering, but that refinement needs item 8. The ratio is the
model-free version of the same idea.

### 5. Disperse forward and reverse siblings

Reversed cards (`r:` IDs) schedule independently, so both directions of
one card can land in the same session, where answering one is a large
hint for the other minutes later. The second success is contaminated
evidence but advances its ladder fully. Anki handles this with sibling
burying and dispersal. At composition, when both directions are due,
serve the more overdue one and push the sibling to the next day. Failing
that, at least maximize their separation within the session.

### 6. Throttle new cards under review debt

The new batch is a flat default of 10 regardless of how many reviews are
due. Introduction rate is the main workload lever in every simulator and
community finding, and new cards taken on a heavy backlog day compound
the backlog. Scale the batch by the due count (full batch under a low
water mark, zero above a high one), always overridable by the explicit
flag.

### 7. Weight production over recognition

The testing-effect literature finds production tests (typed recall)
produce more durable learning than recognition tests (multiple choice).
Today a choice-mode success advances the ladder exactly like a typed
one. The README already warns about this. Options in increasing
strength: cap the rung reachable by choice-mode evidence, require typing
above some interval, or count a choice success as a fractional advance.
All are mode-based rules decided before grading, so they stay inside the
objective-grading stance.

### 8. Per-card memory model (the endgame)

Replace the ladder with a difficulty/stability/retrievability model from
the FSRS family, fit to the review log. State of the art in 2026: FSRS-6
is the Anki default, FSRS-7 is declared the final major version
(fractional intervals, dual power-law forgetting curve). Two facts make
this a natural fit rather than a culture clash:

- FSRS works with binary grades. Its own FAQ says using only Again and
  Good works fine and is sometimes more accurate, and the benchmark
  scores on the pass/fail signal. study's objective grading is exactly
  that signal, with none of the self-grading noise.
- The review log was designed as this model's training data and already
  records state, owed recalls, rung, and overdue-days per answer.

Benchmarked on roughly 1.7 billion reviews, FSRS needs 20 to 30 percent
fewer reviews than SM-2 for the same retention. It also subsumes items
1 and 4 (elapsed time and retrievability are native), replaces the
halve-on-lapse heuristic with modeled post-lapse stability, and unlocks a
desired-retention knob (schedule each card when predicted recall falls to
a target, the workload/retention tradeoff made explicit). An
implementation is small (FSRS fits in ~100 lines, no dependency needed):
ship default parameters first, optimize per deck once the log has a few
thousand events. Neural schedulers (RWKV, LSTM) top the benchmark but are
absurd for a single-user offline tool.

### 9. Calibrate from the review log

Whatever lands, the log adjudicates it. Per-rung recall rate is the
ladder's measured forgetting curve: a rung recalling above ~95 percent is
too short, below ~80 percent too long. Add a small report (extend
`--stats` or a `--calibrate` flag) computing recall by rung, by state,
and by answer mode. This turns items 1 through 7 from taste into
measurement and is the prerequisite for fitting item 8.

**Landed 2026-07-23** as `--calibrate` (`progress.Calibrate`): recall by
rung, by state, and by answer mode, scoped to the deck and direction like
`--stats`.

## Settled refusals

- **Response latency as evidence.** AFK is indistinguishable from
  struggle at this keyboard. The log's `secs` field stays capped and
  diagnostic only. Nothing the scheduler reads.
- **Post-result regrading** (the typo appeal). Grading is the program's
  job. Leniency is automatic and decided before the result is shown,
  never user judgment after it.
- **Self-graded ease buttons** (Again/Hard/Good/Easy). Objective grading
  is the design, and the FSRS results above show the pass/fail signal is
  sufficient, so there is no accuracy argument for adding them.
- **Leech detection.** Rejected. No lapse thresholds, tags, suspension,
  or leech surfacing. Persistently missed cards are already reachable
  through weak-only cram and `--stats`.

## Appendix: the original list (2026-07-19 session)

Preserved verbatim for reference. This is the first version of this list,
produced before the online research above. Items 3 and 8 are now settled
refusals. Item 1 maps to current item 8, item 4 to current item 4, item 5
to current item 7, item 6 to current item 2, item 7 to current item 5.
Item 2 (adaptive relearning criterion) has no successor above and is
still worth considering.

> Ranked by expected learning impact, all compatible with the
> evidence-first philosophy, and numbered for reference:
>
> 1. **Per-card memory modeling.** The single biggest lever. The current
>    ladder is one-size-fits-all: every card climbs the same rungs, and a
>    lapse halves the level. Real cards differ enormously in difficulty,
>    and the review-log data to estimate it already exists (every answer
>    is recorded with timestamps). Even a simple ease factor per card
>    (Anki's approach) beats a fixed ladder; the full version is an
>    FSRS-style model tracking each card's stability and difficulty,
>    scheduling for a target recall probability like 90% instead of fixed
>    steps. This turns "the ladder" into "this card's ladder."
>
> 2. **Adaptive relearning criterion.** A direct extension of your own
>    Rawson & Dunlosky foundation: relearning sessions currently demand
>    one recall from every studied card, but a card with a lapse history
>    has proven that one recall doesn't re-establish it. Cards with
>    repeated lapses should owe two or three relearning recalls, cards
>    with spotless records could stay at one. Cheap to implement,
>    squarely inside the successive-relearning literature.
>
> 3. **Response latency as evidence.** The advantage only study has,
>    since grading is objective: a correct answer after eight hesitant
>    seconds is measurably weaker retrieval than an instant one, and
>    latency-weighted evidence could feed both the session criterion and
>    the ladder. Needs care to normalize per user and per card length so
>    slow typing isn't punished as weak memory.
>
> 4. **Backlog triage.** After a break, the composition rule "most
>    overdue first" is arguably backwards: the most overdue cards are the
>    most likely already forgotten, and their marginal loss from waiting
>    another day is small, while barely-overdue cards are still savable.
>    Ordering a backlog by predicted retention (save what's closest to
>    slipping) and capping the day's bite retains more per minute of a
>    comeback session.
>
> 5. **Mode-aware evidence weighting.** Already designed in this
>    conversation: correct answers in an easier mode than the card's
>    authored one hold the rung instead of climbing it.
>
> 6. **Interval fuzz and load smoothing.** Deterministic intervals make
>    cards learned together stay clumped forever, arriving in the same
>    groups on the same days, which lets group context substitute for
>    memory. A few percent of randomness on due dates breaks the clumps
>    and simultaneously flattens due-count spikes.
>
> 7. **Sibling spacing.** Forward and reverse of the same card on the
>    same day prime each other, booking recognition of a fresh echo as
>    full-strength evidence. Keeping the pair on different days makes the
>    evidence honest.
>
> 8. **Leech detection.** Not a scheduling change so much as a stop-loss:
>    after N lapses the scheduler is provably not the answer, and the
>    card should be flagged for re-authoring (or a confusion pairing)
>    instead of consuming reps forever.

## Sources

- [srs-benchmark](https://github.com/open-spaced-repetition/srs-benchmark)
  and [Expertium's benchmark writeup](https://expertium.github.io/Benchmark.html)
- [FSRS technical explanation](https://expertium.github.io/Algorithm.html)
  and [Anki's algorithm FAQ](https://faqs.ankiweb.net/what-spaced-repetition-algorithm)
- [FSRS-6 release discussion](https://github.com/orgs/open-spaced-repetition/discussions/30)
- [Anki deck options manual](https://docs.ankiweb.net/deck-options.html)
  (fuzz ranges, sort orders)
- [Descending retrievability as default sort](https://github.com/ankitects/anki/issues/3460)
- [fsrs4anki-helper](https://github.com/open-spaced-repetition/fsrs4anki-helper)
  (load balance, disperse siblings, postpone/advance)
- Rawson & Dunlosky,
  [Successive Relearning (2022 review)](https://journals.sagepub.com/doi/full/10.1177/09637214221100484)
- Serfaty & Serrano,
  [The Role of Relearning in L2 Grammar (2024)](https://onlinelibrary.wiley.com/doi/10.1111/lang.12585)
  (initial criterion beyond 3 washed out by relearning)
- Roediger & Karpicke,
  [The Power of Testing Memory (2006)](https://psychnet.wustl.edu/memory/wp-content/uploads/2018/04/Roediger-Karpicke-2006_PPS.pdf)
  (production beats recognition)
- [Spaced Repetition Systems Have Gotten Way Better](https://domenic.me/fsrs/)
- [Implementing FSRS in 100 Lines](https://borretti.me/article/implementing-fsrs-in-100-lines)
