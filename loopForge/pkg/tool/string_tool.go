package tool

// StringTool is a minimal string-in/string-out callable (abstractions.md section 2.4).
type StringTool interface {
	Name() string
	Call(input string) (string, error)
}
