/*
This is mostly an experiment to run an AI generated DST on a very small API.
It seems to work, if it starts to break, feel free to yank it out. We have real
tests to cover everything anyways.
*/

package fault_test

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/unkeyed/unkey/go/pkg/codes"
	"github.com/unkeyed/unkey/go/pkg/fault"
	"github.com/unkeyed/unkey/go/pkg/testutil"
)

var (
	maxDepth     = 1_000
	numTestCases = 10_000

	// Component generation constants.
	numTags       = 20
	numInternals  = 30
	numPublics    = 25
	numBaseErrors = 15

	// String generation constants.
	minWordLength  = 3
	maxWordLength  = 12
	minWordsPerMsg = 2
	maxWordsPerMsg = 8
)

type ErrorComponents struct {
	code       []codes.URN
	internals  []string
	publics    []string
	baseErrors []string
}

// Generator handles all random but deterministic generation.
type Generator struct {
	rng *rand.Rand
}

func NewGenerator(seed int64) *Generator {
	return &Generator{
		rng: rand.New(rand.NewSource(seed)),
	}
}

// generateRandomWord creates a random word of given length.
func (g *Generator) generateRandomWord(minLen, maxLen int) string {
	consonants := "bcdfghjklmnpqrstvwxyz"
	vowels := "aeiou"
	length := g.rng.Intn(maxLen-minLen+1) + minLen

	var word strings.Builder
	useVowel := g.rng.Float32() < 0.5

	for i := 0; i < length; i++ {
		if useVowel {
			word.WriteByte(vowels[g.rng.Intn(len(vowels))])
		} else {
			word.WriteByte(consonants[g.rng.Intn(len(consonants))])
		}
		useVowel = !useVowel
	}

	return word.String()
}

// generateRandomSentence creates a random sentence.
func (g *Generator) generateRandomSentence() string {
	wordCount := g.rng.Intn(maxWordsPerMsg-minWordsPerMsg+1) + minWordsPerMsg
	words := make([]string, wordCount)

	for i := 0; i < wordCount; i++ {
		words[i] = g.generateRandomWord(minWordLength, maxWordLength)
	}

	sentence := strings.Join(words, " ")
	return strings.ToUpper(sentence[:1]) + sentence[1:] + "."
}

// generateRandomTag creates a random error code
func (g *Generator) generateRandomTag() codes.URN {
	words := []string{
		g.generateRandomWord(4, 8),
		g.generateRandomWord(4, 8),
	}
	return codes.URN(strings.ToUpper(strings.Join(words, "_")))
}

// generateComponents creates a complete set of random components.
func (g *Generator) generateComponents() ErrorComponents {
	components := ErrorComponents{
		code:       make([]codes.URN, numTags),
		internals:  make([]string, numInternals),
		publics:    make([]string, numPublics),
		baseErrors: make([]string, numBaseErrors),
	}

	// Generate code
	for i := 0; i < numTags; i++ {
		components.code[i] = g.generateRandomTag()
	}

	// Generate internal messages
	for i := 0; i < numInternals; i++ {
		components.internals[i] = g.generateRandomSentence()
	}

	// Generate public messages
	for i := 0; i < numPublics; i++ {
		components.publics[i] = g.generateRandomSentence()
	}

	// Generate base errors
	for i := 0; i < numBaseErrors; i++ {
		components.baseErrors[i] = g.generateRandomSentence()
	}

	return components
}

// ErrorChainGenerator handles error chain generation using components.
type ErrorChainGenerator struct {
	rng        *rand.Rand
	components ErrorComponents
}

func NewErrorChainGenerator(seed int64) *ErrorChainGenerator {
	gen := NewGenerator(seed)
	return &ErrorChainGenerator{
		rng:        gen.rng,
		components: gen.generateComponents(),
	}
}

func (g *ErrorChainGenerator) generateErrorChain() ([]codes.URN, []string, error) {
	depth := g.rng.Intn(maxDepth) + 1
	usedTags := make([]codes.URN, 0, depth)
	usedMsgs := make([]string, 0, depth)

	baseMsg := g.components.baseErrors[g.rng.Intn(len(g.components.baseErrors))]
	err := fault.New(baseMsg)
	usedMsgs = append(usedMsgs, baseMsg)

	for i := 0; i < depth; i++ {
		wrappers := make([]fault.Wrapper, 0)

		if g.rng.Float32() < 0.7 {
			code := g.components.code[g.rng.Intn(len(g.components.code))]
			wrappers = append(wrappers, fault.Code(code))
			usedTags = append(usedTags, code)
		}

		if g.rng.Float32() < 0.8 {
			internal := g.components.internals[g.rng.Intn(len(g.components.internals))]
			public := g.components.publics[g.rng.Intn(len(g.components.publics))]
			wrappers = append(wrappers, fault.Internal(internal), fault.Public(public))
			usedMsgs = append(usedMsgs, internal)
		}

		if len(wrappers) > 0 {
			err = fault.Wrap(err, wrappers...)
		}
	}

	return usedTags, usedMsgs, err
}

func TestDST(t *testing.T) {
	testutil.SkipUnlessSimulation(t)
	seed := time.Now().UnixNano()
	t.Logf("Using seed: %d", seed)

	generator := NewErrorChainGenerator(seed)

	// Log some sample components for debugging
	t.Logf("Sample generated components:")
	t.Logf("Tags: %v", generator.components.code[:3])
	t.Logf("Internal messages: %v", generator.components.internals[:3])
	t.Logf("Public messages: %v", generator.components.publics[:3])

	for i := 0; i < numTestCases; i++ {
		t.Run(fmt.Sprintf("TestCase_%d", i), func(t *testing.T) {
			expectedTags, expectedMsgs, err := generator.generateErrorChain()

			if err == nil {
				t.Fatal("generated error should not be nil")
			}

			if len(expectedTags) > 0 {
				lastCode := expectedTags[len(expectedTags)-1]
				actualCode, ok := fault.GetCode(err)
				require.True(t, ok)
				if actualCode != lastCode {
					t.Errorf("expected last code%v, got %v", lastCode, actualCode)
				}
			}

			errString := err.Error()
			for _, msg := range expectedMsgs {
				if msg != "" && !strings.Contains(errString, msg) {
					t.Errorf("error string should contain %q, got %q", msg, errString)
				}
			}
		})
	}
}

func TestReproducibility(t *testing.T) {
	seed := time.Now().UnixNano()
	gen1 := NewErrorChainGenerator(seed)
	gen2 := NewErrorChainGenerator(seed)

	for i := 0; i < 10; i++ {
		code1, msgs1, err1 := gen1.generateErrorChain()
		code2, msgs2, err2 := gen2.generateErrorChain()

		if err1.Error() != err2.Error() {
			t.Errorf("Case %d: Errors not identical with same seed", i)
		}

		if !reflect.DeepEqual(code1, code2) {
			t.Errorf("Case %d: Tags not identical with same seed", i)
		}

		if !reflect.DeepEqual(msgs1, msgs2) {
			t.Errorf("Case %d: Messages not identical with same seed", i)
		}
	}
}
