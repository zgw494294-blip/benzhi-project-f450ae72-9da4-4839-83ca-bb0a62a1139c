package domain

func IsEditable(status Status) bool  { return status == StatusDraft || status == StatusRework }
func IsTerminal(status Status) bool  { return status == StatusCredentialed }
func IsImmutable(status Status) bool { return status == StatusApproved || status == StatusCredentialed }
func CanReport(status Status) bool   { return status == StatusInReview }
func CanApprove(status Status) bool  { return status == StatusPendingApproval }

func RuleSetGapThreshold(name string) (int64, bool) {
	switch name {
	case "broadcast-v1":
		return 1000, true
	case "broadcast-v2":
		return 750, true
	default:
		return 0, false
	}
}
