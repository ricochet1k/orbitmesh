package sample

// breakLoop uses break to exit early from a for loop.
func breakLoop(items []int) int {
	for _, v := range items {
		if v < 0 {
			break
		}
		_ = v
	}
	return 0
}

// continueLoop uses continue to skip negative values.
func continueLoop(items []int) int {
	total := 0
	for _, v := range items {
		if v < 0 {
			continue
		}
		total += v
	}
	return total
}

// earlyReturn has code after a return that is unreachable.
func earlyReturn(n int) int {
	if n > 0 {
		return n
	}
	return -n
}

// unreachableAfterReturn demonstrates dead code after an unconditional return.
func unreachableAfterReturn() int {
	return 1
	_ = 42 // unreachable
	return 2
}
