// Command multipart demonstrates the unified Marshal/Unmarshal API with the
// goform.File type for file uploads. It builds a multipart body client-side
// and decodes it server-side without coupling to net/http.
package main

import (
	"fmt"

	"github.com/elsharaky/goform"
)

type Upload struct {
	Title  string        `form:"title"`
	Avatar goform.File   `form:"avatar"`
	Docs   []goform.File `form:"documents"`
}

func main() {
	client := Upload{
		Title:  "My Submission",
		Avatar: goform.File{Content: []byte("avatar-bytes"), ContentType: "image/png", Filename: "me.png"},
		Docs: []goform.File{
			{Content: []byte("resume"), ContentType: "text/plain", Filename: "resume.txt"},
		},
	}

	// Client side: struct -> body + Content-Type. goform auto-detects that the
	// struct contains File fields and produces multipart/form-data.
	body, contentType, err := goform.Marshal(client)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Client multipart body (%d bytes), content-type: %s\n", len(body), contentType)

	// Server side: body + Content-Type -> struct. No net/http dependency and no
	// manual ParseMultipartForm call required.
	var server Upload
	if err := goform.Unmarshal(body, contentType, &server); err != nil {
		panic(err)
	}

	fmt.Printf("Server title: %s\n", server.Title)
	fmt.Printf("Server avatar: %s (%s)\n", string(server.Avatar.Content), server.Avatar.Filename)
	for _, d := range server.Docs {
		fmt.Printf("Server doc: %s (%s)\n", string(d.Content), d.Filename)
	}
}
