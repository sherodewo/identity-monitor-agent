package sentiment

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

type wordPieceTokenizer struct {
	vocab map[string]int64
	unk   int64
	cls   int64
	sep   int64
	pad   int64
}

func loadTokenizer(path string) (*wordPieceTokenizer, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	vocab := map[string]int64{}
	scanner := bufio.NewScanner(f)
	var index int64
	for scanner.Scan() {
		token := strings.TrimSpace(scanner.Text())
		if token != "" {
			vocab[token] = index
		}
		index++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	required := []string{"[UNK]", "[CLS]", "[SEP]", "[PAD]"}
	for _, token := range required {
		if _, ok := vocab[token]; !ok {
			return nil, fmt.Errorf("vocabulary missing %s", token)
		}
	}
	return &wordPieceTokenizer{vocab: vocab, unk: vocab["[UNK]"], cls: vocab["[CLS]"], sep: vocab["[SEP]"], pad: vocab["[PAD]"]}, nil
}

func basicTokens(text string) []string {
	text = strings.ToLower(norm.NFD.String(text))
	var words []string
	var current []rune
	flush := func() {
		if len(current) > 0 {
			words = append(words, string(current))
			current = current[:0]
		}
	}
	for _, r := range text {
		if unicode.Is(unicode.Mn, r) || unicode.IsControl(r) && !unicode.IsSpace(r) {
			continue
		}
		if unicode.IsSpace(r) {
			flush()
			continue
		}
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			flush()
			words = append(words, string(r))
			continue
		}
		current = append(current, r)
	}
	flush()
	return words
}

func (t *wordPieceTokenizer) pieces(word string) []int64 {
	runes := []rune(word)
	if len(runes) > 100 {
		return []int64{t.unk}
	}
	var ids []int64
	for start := 0; start < len(runes); {
		found := false
		for end := len(runes); end > start; end-- {
			piece := string(runes[start:end])
			if start > 0 {
				piece = "##" + piece
			}
			if id, ok := t.vocab[piece]; ok {
				ids = append(ids, id)
				start = end
				found = true
				break
			}
		}
		if !found {
			return []int64{t.unk}
		}
	}
	return ids
}

func (t *wordPieceTokenizer) encode(text string, maxTokens int) (ids, mask, types []int64) {
	ids = append(ids, t.cls)
	for _, word := range basicTokens(text) {
		pieces := t.pieces(word)
		if len(ids)+len(pieces)+1 > maxTokens {
			break
		}
		ids = append(ids, pieces...)
	}
	ids = append(ids, t.sep)
	mask = make([]int64, maxTokens)
	types = make([]int64, maxTokens)
	for i := range ids {
		mask[i] = 1
	}
	for len(ids) < maxTokens {
		ids = append(ids, t.pad)
	}
	return ids, mask, types
}
