package main

import (
	"fmt"

	"github.com/ChickenBenny/Gaslight/internal/version"
)

func main() {
	fmt.Printf("gaslight %s\n", version.Version)
}
