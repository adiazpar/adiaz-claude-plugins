# Principle: Pressure-test before commitment

Every strategic recommendation produced by research feels true at the moment you read it. That feeling is unreliable. The recommendation is built from desk research, citations the agent thought were relevant, and a synthesis the agent had to perform under a word limit. Some of those decisions were wrong. You don't yet know which ones.

Pressure-testing is the practice of running an adversarial review of every recommendation before committing engineering or marketing work to it. Form a hypothesis. Then deliberately try to kill it. Use a second agent if you used a first agent for the original recommendation. Use a different framing.

## What pressure-testing catches

The recommendation that produced a pivot proposal in this methodology had five load-bearing claims. The pressure test found that three were false:

1. A claim of "no incumbent" was disproved by surfacing a direct competitor the first agent missed.
2. A claim of "1–2 month engineering cost" was disproved by reading the actual schema, which had a structural mismatch making it 3–5 months.
3. A claim of "X is a moat" was disproved by tracking the specific tools that already own the differentiator and what they charge.

Each of those false claims would have led to months of misdirected work. The pressure test cost two hours of agent time. The ratio is the entire reason to do this.

## How to structure the pressure test

Give the adversarial agent four things:

1. **The exact recommendation under attack.** Paste it. Include the load-bearing claims explicitly.
2. **The constraints the original research was supposed to respect.** Founder location, funding state, technical stack, time budget, language fluency, distribution capacity.
3. **A list of specific claims to scrutinize.** "Is X really a moat?" "Is Y really the size you said?" "Did you check competitor Z?" Force the agent to evaluate specific claims rather than vibe-checking the whole.
4. **A list of "missed investigations."** Categories the first scan might have skipped. Surface 3–6 specific candidates the original scan didn't cover.

End the prompt with explicit permission to kill the recommendation. Tell the agent: "Be willing to recommend against the pivot. Do not soften."

## How to read the result

The pressure test is useful regardless of whether it kills the recommendation. Three outcomes, in order of likelihood:

1. **Most claims survive, one or two get refined.** This is the most common case. The recommendation stands but with sharper edges. Worth running customer discovery on this version.

2. **Several claims fall, the recommendation becomes "conditional."** The pressure test found real flaws. Re-evaluate: is the recommendation still attractive after the corrections, or does the corrected version look like a worse opportunity?

3. **The recommendation dies entirely.** Less common but real. The pressure test found a killer — a hidden incumbent, a structural mismatch, a regulatory cliff. Move to the next option.

In case 3, do not run pass N+1 immediately. Pause. Read the meta-pattern recognition principle.

## What pressure-testing is not

It is not a debate. The adversarial agent is not your enemy. The point is not to "win" against the recommendation; the point is to find what's wrong with it cheaply.

It is not a substitute for customer interviews. Pressure-testing kills bad recommendations before they reach customers. It does not validate good ones.

It is not infallible. Two passes can both be wrong. If the first agent missed Conventory, the pressure-test agent could also miss it. The mitigation is to force both agents to actively search for missed competitors rather than asking "is this comprehensive?"

## When to skip pressure-testing

When the recommendation is to do nothing or to walk away. Negative recommendations don't need adversarial review — they're already maximally cautious. If the agent says "this market isn't worth pursuing," you don't need a second agent to attack that conclusion. Customer interviews or moving to the next investigation is the right next step.
