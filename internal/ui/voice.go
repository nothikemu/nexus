package ui

import (
	"hash/fnv"
	"time"
)

// The voice is the small catalogue of things Nexus says. Personality lives
// in a handful of carefully chosen lines; everything else is plain
// information.
//
// Rules: lowercase, short, calm, confident. No exclamation marks, no emoji,
// never sarcastic. At most one line of personality per command.

// The moments Nexus has something to say about.
const (
	MomentAwake      = "awake"
	MomentReady      = "ready"
	MomentConnected  = "connected"
	MomentNice       = "nice"
	MomentBetter     = "better"
	MomentSynced     = "synced"
	MomentAsleep     = "asleep"
	MomentGoodbye    = "goodbye"
	MomentNoticed    = "noticed"
	MomentBroke      = "broke"
	MomentAllGood    = "allgood"
	MomentNothingNew = "nothingnew"
)

var phrases = map[string][]string{
	MomentAwake:      {"nexus is awake.", "nexus is up.", "nexus is awake."},
	MomentReady:      {"ready when you are.", "you're ready.", "ready."},
	MomentConnected:  {"everything is connected.", "all systems connected."},
	MomentNice:       {"nice.", "done.", "nice."},
	MomentBetter:     {"that's better.", "much better."},
	MomentSynced:     {"everything is synced.", "all in sync."},
	MomentAsleep:     {"nexus is asleep.", "the database is resting."},
	MomentGoodbye:    {"see you soon.", "resting now.", "goodnight."},
	MomentNoticed:    {"nexus noticed something.", "worth a look."},
	MomentBroke:      {"something broke.", "that didn't work."},
	MomentAllGood:    {"all good.", "looking healthy."},
	MomentNothingNew: {"nothing to do.", "already up to date."},
}

// Say picks a phrase for a moment. Selection is stable within a minute so a
// command never flickers between phrasings, but varies across sessions.
func Say(moment string) string {
	options := phrases[moment]
	if len(options) == 0 {
		return ""
	}
	h := fnv.New32a()
	h.Write([]byte(moment))
	h.Write([]byte(time.Now().Format("2006-01-02T15:04")))
	return options[int(h.Sum32())%len(options)]
}

// SayFirst returns the canonical (first) phrase for a moment; used in tests
// and plain mode where stability matters more than variety.
func SayFirst(moment string) string {
	if options := phrases[moment]; len(options) > 0 {
		return options[0]
	}
	return ""
}
