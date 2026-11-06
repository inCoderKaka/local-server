# Plan window calibration

How much of the 5-hour plan window one session used, measured in tokens. Budget rules that quote a share of the window use these numbers.

```
tokens_per_window: 6201248
counted: 5891186
fraction_of_window_used: 0.95
calibrated_from_session: 7fa4e93b-9dc5-4ba4-bba6-199e13d01dc2
date: 2026-11-06
how_counted: input + output + cache-creation tokens of every reply in the project's transcripts, including sub-agents, deduplicated by message id; cache reads and OpenCode are excluded. Script: budget-count.py in the orchestrate-implement skill directory, run with --session <id>.
cost_proxy_usd_per_window: 29.03
```

The user reported that the session above used 95% of the window. The script counted 5,891,186 tokens for that session, so one full window is 5,891,186 / 0.95 = 6,201,248 tokens (rounded). To turn a token count into a share of the window, divide it by `tokens_per_window`.

`cost_proxy_usd_per_window` is the API list-price cost of the same window. It is a secondary cross-check only: use the token count as the measure, and use the dollar figure to sanity-check it.
