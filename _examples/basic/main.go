// Command basic demonstrates the simplest goform usage: marshalling a struct
// to a body + Content-Type and unmarshalling it back with the unified API.
package main

import (
	"fmt"

	"github.com/elsharaky/goform"
)

type User struct {
	Name  string `form:"name"`
	Email string `form:"email"`
	Age   int    `form:"age"`
}

func main() {
	// Marshal struct -> body bytes + Content-Type.
	body, ct, err := goform.Marshal(User{
		Name:  "Alice",
		Email: "alice@example.com",
		Age:   30,
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("Content-Type: %s\nBody: %s\n", ct, body)

	// Unmarshal body + Content-Type -> struct (auto-detects urlencoded here).
	var user User
	if err := goform.Unmarshal(body, ct, &user); err != nil {
		panic(err)
	}
	fmt.Printf("Unmarshalled: %+v\n", user)
}
