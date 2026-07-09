package app

import goaway "github.com/TwiN/go-away"

// isInappropriate reports whether a player name contains profanity or slurs.
// go-away normalizes leetspeak / special characters and carries a false-positive
// whitelist (e.g. "class", "assassin"), so this catches obfuscated variants
// without tripping on innocent substrings.
func isInappropriate(name string) bool {
	return goaway.IsProfane(name)
}
