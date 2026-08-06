package evals

func validEvalPhase(phase string) bool {
	return phase == "plan" || phase == "delivery" || phase == "monitor"
}
