package sample

// Greeting is the canonical greeting.
type Greeting struct {
	Text string
}

// Hello returns a greeting for name.
func Hello(name string) string {
	return "hello " + name
}

// Greet renders the greeting.
func (g Greeting) Greet() string {
	return g.Text
}
