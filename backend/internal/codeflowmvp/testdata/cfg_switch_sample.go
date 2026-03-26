package sample

func switchSample(n int) string {
	switch n {
	case 1:
		return "one"
	case 2:
		return "two"
	default:
		return "other"
	}
}

func switchNoDefault(n int) int {
	result := 0
	switch n {
	case 0:
		result = 10
	case 1:
		result = 20
	}
	return result
}
