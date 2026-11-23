package tonight

import (
	"encoding/json"
	"errors"
)

// reasonLength is the longest reason shown. The prompt asks for 140
// characters; this is the hard stop for an answer that ignored it.
const reasonLength = 200

// What can be wrong with an answer. Neither error repeats any of it: an
// answer is model output and must not reach the logs.
var (
	errAnswerNotJSON  = errors.New("the model's answer was not the JSON it was asked for")
	errAnswerNoPicks  = errors.New("the model's answer named no title from the list")
	errAnswerTooLarge = errors.New("the model's answer was larger than any valid answer")
)

// maxAnswerBytes is far more than a valid answer needs (three picks of a
// sentence each) and far less than a runaway one.
const maxAnswerBytes = 16 << 10

// choice is one validated pick: a position in the list that was sent
// (starting at 0) and the reason to show with it.
type choice struct {
	position int
	reason   string
}

// parseAnswer turns the model's answer into at most MaxPicks choices among
// the count titles it was shown.
//
// The answer is untrusted, structured output or not. The only things taken
// from it are indexes, checked against the list that was sent, and reasons,
// reduced to a single line of bounded length for display. An index that is
// out of range or repeated is dropped; a pick whose reason is empty keeps an
// empty reason for the caller to fill. An answer with no valid pick at all
// is an error.
func parseAnswer(raw string, count int) ([]choice, error) {
	if len(raw) > maxAnswerBytes {
		return nil, errAnswerTooLarge
	}
	var answer struct {
		Picks []struct {
			Index  int    `json:"index"`
			Reason string `json:"reason"`
		} `json:"picks"`
	}
	if err := json.Unmarshal([]byte(raw), &answer); err != nil {
		return nil, errAnswerNotJSON
	}

	seen := make(map[int]bool, MaxPicks)
	var choices []choice
	for _, pick := range answer.Picks {
		if len(choices) == MaxPicks {
			break
		}
		if pick.Index < 1 || pick.Index > count || seen[pick.Index] {
			continue
		}
		seen[pick.Index] = true
		choices = append(choices, choice{
			position: pick.Index - 1,
			reason:   clip(cleanText(pick.Reason), reasonLength),
		})
	}
	if len(choices) == 0 {
		return nil, errAnswerNoPicks
	}
	return choices, nil
}
