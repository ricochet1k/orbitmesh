package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("OrbitMesh Agent Orchestration System")
	fmt.Println("Starting server...")

	if len(os.Args) > 1 {
		fmt.Printf("Command: %s\n", os.Args[1])
	}
}
