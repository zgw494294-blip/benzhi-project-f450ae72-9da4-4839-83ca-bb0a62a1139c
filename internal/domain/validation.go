package domain

import (
	"fmt"
	"strings"
)

func ValidateMetadata(title, language, batch, rules string, duration int64) error {
	if strings.TrimSpace(title) == "" || strings.TrimSpace(language) == "" || strings.TrimSpace(batch) == "" || strings.TrimSpace(rules) == "" {
		return fmt.Errorf("%w:节目元数据不完整", ErrValidation)
	}
	if duration <= 0 {
		return fmt.Errorf("%w:时长必须为正数", ErrValidation)
	}
	if _, ok := RuleSetGapThreshold(rules); !ok {
		return fmt.Errorf("%w:规则集无效", ErrValidation)
	}
	return nil
}
