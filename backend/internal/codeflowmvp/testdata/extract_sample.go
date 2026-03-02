package sample

type worker struct{}

func helper() {}

func (w *worker) run(items []int) {
	helper()
	for _, item := range items {
		helper()
		go helper()
		w.step(item)
	}
	go w.step(1)
}

func (w worker) step(v int) {}
