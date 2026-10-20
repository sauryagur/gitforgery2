package tui

type stepStatus int

const (
	stepPending stepStatus = iota
	stepActive
	stepDone
)

const (
	stepRange = iota
	stepInstruction
	stepRun
	stepReview
	stepValidate
	stepPush
	stepCount
)

var stepTitles = [stepCount]string{"Select Range", "Write Instruction", "Run Harness", "Review Patches", "Validate Result", "Confirm Push"}

type logLine struct{ At, Stage, Text string }

type patchItem struct {
	Index     int
	Title     string
	Files     int
	Additions int
	Deletions int
}

type diffKind int

const (
	diffContext diffKind = iota
	diffAdd
	diffDel
	diffHunk
	diffHeader
)

type diffLine struct {
	Kind diffKind
	Text string
}

type workspaceView struct {
	Width, Height int

	Branch  string
	Session string
	Pushed  bool

	Step       int
	StepStatus [stepCount]stepStatus

	Commits   int
	Files     int
	Additions int
	Deletions int

	Harness        string
	HarnessVersion string
	HarnessStatus  string
	HarnessRunning bool

	RangeDesc      string
	PatchCount     int
	FinalTreeMatch bool

	Logs    []logLine
	Elapsed string

	Instruction        string
	InstructionLine    int
	InstructionEditing bool
	InstructionErr     string

	Patches      []patchItem
	Selected     int
	ListStart    int
	Diff         []diffLine
	DiffOffset   int
	DiffTotal    int
	ShowOriginal bool
	EditingPatch bool
	ReviewStatus string

	HelpVisible  bool
	ModalVisible bool
	ModalTitle   string
	ModalBody    string
	ModalHint    string
	RangeValue   string
	RangeErr     string
	Harnesses    []string
	HarnessIdx   int
}
