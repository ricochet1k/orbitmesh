package sample

func cfgSample(n int) int {
	total := 0
	if n > 0 {
		total = n
	} else {
		total = 1
	}
	for i := 0; i < n; i++ {
		total += i
	}
	return total
}
