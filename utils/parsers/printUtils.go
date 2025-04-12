package parsers

import (
	"fmt"
	"strings"
)

func Struct(input interface{}) (msg string) {
	msg = fmt.Sprintf("%T%+v", input, input)
	msg = strings.ReplaceAll(msg, " ", "\n    ")
	msg = strings.ReplaceAll(msg, "{", " {\n    ")
	msg = strings.ReplaceAll(msg, "}", "\n}\n")
	return
}
