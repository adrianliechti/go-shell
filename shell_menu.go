package shell

// MenuItem describes an application command in a native menu. A separator
// ignores all other fields.
type MenuItem struct {
	Title     string `json:"title,omitempty"`
	Command   string `json:"command,omitempty"`
	Key       string `json:"key,omitempty"`
	Shift     bool   `json:"shift,omitempty"`
	Alt       bool   `json:"alt,omitempty"`
	Control   bool   `json:"control,omitempty"`
	Disabled  bool   `json:"disabled,omitempty"`
	Separator bool   `json:"separator,omitempty"`
}
