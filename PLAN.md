# PLAN.md

# Routes

## `/tv` – Event Display
TV has been completed.

## `/game` – Play the Game
 Game has been completed.
 
TODO:
- slow down game X acc a bit

DONE:
- filter naughty names (go-away profanity filter; rejected at /api/player and /api/scores/submit)
- each name occurs once per leaderboard row (upsert keeps highest score per name, case-insensitive)
