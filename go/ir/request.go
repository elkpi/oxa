package ir

import "encoding/json"

// Role is a message role. Closed set: user | assistant (spec/01 s3.3).
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Request is a conversation to be sent to a model, face-neutral (spec/01
// s3.1). specVersion is a document-layer field and is stamped by the
// canonical codec, not stored here.
type Request struct {
	Model      string
	System     []SystemBlock
	Messages   []Message
	Tools      []Tool
	ToolChoice *ToolChoice
	Params     Params
	Metadata   map[string]string
}

// Message is one conversational turn.
type Message struct {
	Role    Role
	Content []Block
}

// Tool is a tool definition. InputSchema is a JSON-Schema-shaped object
// carried verbatim (json.RawMessage holding the object); implementations MUST
// NOT analyze or rewrite it (spec/01 s3.5).
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// ToolChoice selects tool-usage behavior (spec/01 s3.6). Name is set iff Mode
// is "tool".
type ToolChoice struct {
	Mode string // auto | any | tool | none
	Name string
}

const (
	ToolChoiceAuto = "auto"
	ToolChoiceAny  = "any"
	ToolChoiceTool = "tool"
	ToolChoiceNone = "none"
)

// Params are sampling parameters (spec/01 s3.7). Pointer types mean absence
// is meaningful; absent and zero are different states. Ranges are face
// concerns.
type Params struct {
	Temperature   *float64
	TopP          *float64
	MaxTokens     *int64
	StopSequences []string
}

// set reports whether any parameter carries a value; the canonical codec
// omits the whole params object when it does not.
func (p Params) set() bool {
	return p.Temperature != nil || p.TopP != nil || p.MaxTokens != nil || len(p.StopSequences) > 0
}
