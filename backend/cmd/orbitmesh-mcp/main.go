package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("OrbitMesh MCP Server")
	fmt.Println("Starting MCP server...")

	if len(os.Args) > 1 {
		fmt.Printf("Command: %s\n", os.Args[1])
	}
}
