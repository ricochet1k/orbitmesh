package sample

func first(n int) int {
	total := 0
	if n > 0 {
		total = n
	}
	return total
}

func second(n int) int {
	for i := 0; i < n; i++ {
		n += i
	}
	return n
}
