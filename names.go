package main

import (
	"fmt"
	"os"
	"strings"
)

// Gamer-style username components. Combinations produce names like
// xX_ShadowBlade_Xx, KingSlayer99, iTzZach, NoobSlayer_YT.
var gamerWords = []string{
	"Shadow", "Blade", "Night", "Wolf", "Fury", "Storm", "Ghost", "Legend",
	"Reaper", "Ninja", "Dragon", "Phoenix", "Hunter", "Viper", "Titan", "Venom",
	"Slayer", "Wraith", "Savage", "Rogue", "Joker", "Tiger", "Falcon", "Hawk",
	"Bear", "Snake", "Phantom", "Boss", "King", "Queen", "Prince",
	"Pro", "Noob", "Epic", "Mega", "Ultra", "Dark", "Light", "Crazy",
	"Swift", "Lucky", "Silent", "Royal", "Iron", "Golden", "Pixel", "Cyber",
	"Rocket", "Rapid", "Blaze", "Frost", "Thunder", "Sky", "Fire", "Ice",
}

var gamerTags = []string{
	"xX", "Xx", "iTz", "Zx", "Mr", "No", "Im", "Yo", "Boss", "King",
	"Pro", "Mc", "PvP", "OG", "TTV", "YT", "MC", "Glory", "Raptor", "Sniper",
}

var gamerSuffixes = []string{
	"Xx", "xX", "_YT", "_TTV", "_MC", "YT", "TTV", "_PvP", "Pro", "_OG",
	"__", "2010", "99", "2000", "07", "123", "21", "_King", "_Legend",
}

const maxNameLen = 16

// makeName returns the i-th unique gamer-style username (<=16 chars).
//
// Names come from four non-overlapping blocks so the result is unique for any
// two distinct indices:
//
//	block 0: tag_word_suf   (tag x word x suf)
//	block 1: tagWord        (tag x word)
//	block 2: wordSuf        (word x suf)
//	block 3: word##         (word x 90 numbers)
func makeName(i int) string {
	nTags, nWords, nSuf := len(gamerTags), len(gamerWords), len(gamerSuffixes)
	block0 := nTags * nWords * nSuf
	block1 := nTags * nWords
	block2 := nWords * nSuf
	block3 := nWords * 90
	total := block0 + block1 + block2 + block3
	i %= total

	switch {
	case i < block0:
		t := i / (nWords * nSuf)
		r := i % (nWords * nSuf)
		w, s := r/nSuf, r%nSuf
		return truncate(gamerTags[t] + "_" + gamerWords[w] + "_" + gamerSuffixes[s])
	case i < block0+block1:
		r := i - block0
		return truncate(gamerTags[r/nWords] + gamerWords[r%nWords])
	case i < block0+block1+block2:
		r := i - block0 - block1
		return truncate(gamerWords[r/nSuf] + gamerSuffixes[r%nSuf])
	default:
		r := i - block0 - block1 - block2
		return truncate(fmt.Sprintf("%s%d", gamerWords[r/90], 10+r%90))
	}
}

func truncate(name string) string {
	if len(name) > maxNameLen {
		name = name[:maxNameLen]
	}
	return name
}

// writeNamesFile writes one username per line to the given path.
func writeNamesFile(path string, count int) error {
	var sb strings.Builder
	for i := 0; i < count; i++ {
		sb.WriteString(makeName(i))
		sb.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}
