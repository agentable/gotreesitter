package grammargen

import (
	"time"

	"github.com/agentable/gotreesitter"
)

func generateDartParityLanguageWithTimeout(gram *Grammar, timeout time.Duration) (*gotreesitter.Language, error) {
	gram.EnableLRSplitting = true
	return generateWithTimeout(gram, timeout)
}
