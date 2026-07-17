//go:build wallet_probe
// +build wallet_probe

package main

import (
	"bytes"
	"compress/zlib"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/tyler-smith/go-bip39"
)

var targetAddresses = map[string]string{
	"MSC017d78d2c1920db5321271a2d594a4995a3c5ba99d": "foundation/A",
	"MSC01102bdf87789381354be6ec8af1f49688306ea83c": "treasury/B",
	"MSC01dc7b2c81d1211199f209a52a9688a31352f3b800": "validator/C",
	"MSC01d8f4952c11e683aac3cf6652513cd90982e4a938": "validator/D",
}

var targetPublicKeys = map[string]string{
	"3adadb92850e85603bf122c0bc757987ec633945d5773f4ccc28853e7a9a5978": "foundation/A",
	"e985a04375642887373ffb1b217843ca294d425d53d6ef6c7c86872534618e6f": "treasury/B",
	"c5f1f3c40667f8430ed528f2176e68f3cb889292aa0ade25df4ecbdc86b217c8": "validator/C",
	"acaf8386bd82afa3e2867b3f2a10580a076d35514bc5d1f62b0866d4df53eff7": "validator/D",
}

func main() {
	home, _ := os.UserHomeDir()
	paths := []string{
		filepath.Join(home, "Downloads", "msc-wallet-seed.pdf"),
		filepath.Join(home, "Downloads", "msc-wallet-seed (1).pdf"),
		filepath.Join(home, "Downloads", "seed.txt"),
		filepath.Join(home, "Downloads", "pass sed msc wallet.txt"),
	}

	textByPath := make(map[string]string)
	var combined strings.Builder
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		text := extractText(raw, strings.HasSuffix(strings.ToLower(path), ".pdf"))
		textByPath[path] = text
		combined.WriteString("\n")
		combined.WriteString(text)
	}

	mnemonics := candidateMnemonics(textByPath)
	passwords := candidatePasswords(combined.String())
	if len(passwords) == 0 {
		passwords = []string{""}
	}

	type match struct {
		Address       string
		Label         string
		PublicKey     string
		MnemonicFile  string
		PasswordIndex int
		Account       uint32
		Change        uint32
		Index         uint32
	}
	var matches []match
	for _, m := range mnemonics {
		for pi, pass := range passwords {
			seed := bip39.NewSeed(m.phrase, pass)
			for account := uint32(0); account <= 2; account++ {
				for change := uint32(0); change <= 1; change++ {
					for index := uint32(0); index <= 20; index++ {
						pub, err := derivePublic(seed, account, change, index)
						if err != nil {
							continue
						}
						pubHex := hex.EncodeToString(pub)
						addr := addressFromPublicKey(pub, "91938")
						label := targetAddresses[addr]
						if label == "" {
							label = targetPublicKeys[pubHex]
						}
						if label == "" {
							continue
						}
						matches = append(matches, match{
							Address:       addr,
							Label:         label,
							PublicKey:     pubHex,
							MnemonicFile:  filepath.Base(m.source),
							PasswordIndex: pi,
							Account:       account,
							Change:        change,
							Index:         index,
						})
					}
				}
			}
		}
	}

	fmt.Printf("candidate_mnemonics=%d candidate_passwords=%d matches=%d\n", len(mnemonics), len(passwords), len(matches))
	for _, m := range matches {
		fmt.Printf("MATCH label=%s address=%s public_key=%s source=%s password_candidate_index=%d path=m/44'/91938'/%d'/%d'/%d'\n",
			m.Label, m.Address, m.PublicKey, m.MnemonicFile, m.PasswordIndex, m.Account, m.Change, m.Index)
	}
}

type mnemonicCandidate struct {
	phrase string
	source string
}

func candidateMnemonics(textByPath map[string]string) []mnemonicCandidate {
	seen := make(map[string]mnemonicCandidate)
	lengths := []int{12, 15, 18, 21, 24}
	for path, text := range textByPath {
		tokens := wordTokens(text)
		for _, n := range lengths {
			if len(tokens) < n {
				continue
			}
			for i := 0; i+n <= len(tokens); i++ {
				phrase := strings.Join(tokens[i:i+n], " ")
				if bip39.IsMnemonicValid(phrase) {
					seen[phrase] = mnemonicCandidate{phrase: phrase, source: path}
				}
			}
		}
	}
	out := make([]mnemonicCandidate, 0, len(seen))
	for _, cand := range seen {
		out = append(out, cand)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].source < out[j].source
	})
	return out
}

func candidatePasswords(text string) []string {
	seen := map[string]struct{}{"": {}}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || len(line) > 160 {
			continue
		}
		seen[line] = struct{}{}
		for _, sep := range []string{":", "=", "-"} {
			if idx := strings.Index(line, sep); idx >= 0 {
				right := strings.TrimSpace(line[idx+1:])
				if right != "" && len(right) <= 160 {
					seen[right] = struct{}{}
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for pass := range seen {
		out = append(out, pass)
	}
	sort.Strings(out)
	return out
}

func extractText(raw []byte, pdf bool) string {
	var parts []string
	parts = append(parts, printableText(raw, !pdf))
	for _, stream := range pdfStreams(raw) {
		if inflated, err := inflate(stream); err == nil {
			parts = append(parts, printableText(inflated, true))
		}
	}
	return strings.Join(parts, "\n")
}

func pdfStreams(raw []byte) [][]byte {
	var streams [][]byte
	start := []byte("stream")
	end := []byte("endstream")
	rest := raw
	for {
		i := bytes.Index(rest, start)
		if i < 0 {
			break
		}
		rest = rest[i+len(start):]
		rest = bytes.TrimLeft(rest, "\r\n")
		j := bytes.Index(rest, end)
		if j < 0 {
			break
		}
		streams = append(streams, bytes.TrimSpace(rest[:j]))
		rest = rest[j+len(end):]
	}
	return streams
}

func inflate(raw []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func printableText(raw []byte, keepLines bool) string {
	var b strings.Builder
	for _, r := range string(raw) {
		switch {
		case keepLines && (r == '\n' || r == '\r'):
			b.WriteRune('\n')
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		case strings.ContainsRune("'_-/=:.@", r):
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	return b.String()
}

func wordTokens(text string) []string {
	re := regexp.MustCompile(`[a-z]+`)
	return re.FindAllString(strings.ToLower(text), -1)
}

func derivePublic(seed []byte, account, change, index uint32) (ed25519.PublicKey, error) {
	key, cc, err := slip10Master(seed)
	if err != nil {
		return nil, err
	}
	for _, part := range []uint32{44, 91938, account, change, index} {
		key, cc, err = slip10Child(key, cc, part+0x80000000)
		if err != nil {
			return nil, err
		}
	}
	priv := ed25519.NewKeyFromSeed(key)
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("bad public key")
	}
	out := make([]byte, len(pub))
	copy(out, pub)
	return out, nil
}

func slip10Master(seed []byte) ([]byte, []byte, error) {
	mac := hmac.New(sha512.New, []byte("ed25519 seed"))
	if _, err := mac.Write(seed); err != nil {
		return nil, nil, err
	}
	out := mac.Sum(nil)
	return append([]byte(nil), out[:32]...), append([]byte(nil), out[32:]...), nil
}

func slip10Child(key, cc []byte, child uint32) ([]byte, []byte, error) {
	data := make([]byte, 1+32+4)
	copy(data[1:], key)
	binary.BigEndian.PutUint32(data[33:], child)
	mac := hmac.New(sha512.New, cc)
	if _, err := mac.Write(data); err != nil {
		return nil, nil, err
	}
	out := mac.Sum(nil)
	return append([]byte(nil), out[:32]...), append([]byte(nil), out[32:]...), nil
}

func addressFromPublicKey(pub ed25519.PublicKey, chainID string) string {
	payload := append([]byte("MSC-ADDR|"+strings.TrimSpace(chainID)+"|"), pub...)
	h1 := sha256.Sum256(payload)
	h2 := sha256.Sum256(h1[:])
	out := make([]byte, 21)
	out[0] = 0x01
	copy(out[1:], h2[:20])
	return "MSC" + hex.EncodeToString(out)
}
