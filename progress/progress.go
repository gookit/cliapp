package progress

import (
	"fmt"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// internal format for Progress
const (
	MinFormat  = "{@message}{@current}/{@max}"
	TxtFormat  = "{@message}{@percent:4s}%({@current}/{@max})"
	DefFormat  = "{@message}{@percent:4s}%({@current}/{@max})"
	FullFormat = "{@percent:4s}%({@current}/{@max}) {@elapsed:6s}/{@estimated:-6s} {@memory:6s}"
)

var builtinWidgets = map[string]WidgetFunc{
	"elapsed": func(p ProgressFace) string { // 消耗时间
		// fmt.Sprintf("%.3f", time.Since(startTime).Seconds()*1000)
		sec := time.Since(p.(*Progress).StartedAt()).Seconds()
		return HowLongAgo(int64(sec))
	},
	"remaining": func(pf ProgressFace) string { // 剩余时间
		p := pf.(*Progress)
		step := p.Progress() // current progress

		// not set max steps OR current progress is 0
		if p.MaxSteps == 0 || step == 0 {
			return "unknown"
		}

		// calc remaining time
		sec64 := int64(time.Since(p.StartedAt()).Seconds())
		remaining := uint(sec64) / step * (p.MaxSteps - step)
		return HowLongAgo(int64(remaining))
	},
	"estimated": func(pf ProgressFace) string { // 计算总的预计时间
		p := pf.(*Progress)
		step := p.Progress() // current progress

		// not set max steps OR current progress is 0
		if p.MaxSteps == 0 || step == 0 {
			return "unknown"
		}

		// calc estimated time
		sec64 := int64(time.Since(p.StartedAt()).Seconds())
		estimated := uint(sec64) / step * p.MaxSteps
		return HowLongAgo(int64(estimated))
	},
	"memory": func(pf ProgressFace) string {
		mem := new(runtime.MemStats)
		runtime.ReadMemStats(mem)
		return formatMemoryVal(mem.Sys)
	},
	"max": func(pf ProgressFace) string {
		return fmt.Sprint(pf.(*Progress).MaxSteps)
	},
	"current": func(pf ProgressFace) string {
		p := pf.(*Progress)
		step := fmt.Sprint(p.Progress())
		width := fmt.Sprint(p.StepWidth)
		diff := len(width) - len(step)
		if diff <= 0 {
			return step
		}

		return strings.Repeat(" ", diff) + step
	},
	"percent": func(pf ProgressFace) string {
		return fmt.Sprintf("%.1f", pf.(*Progress).Percent() * 100)
	},
}

// use for match like "{@bar}" "{@percent:3s}"
var widgetMatch = regexp.MustCompile(`{@([\w]+)(?::([\w-]+))?}`)

// WidgetFunc handler func for progress widget
type WidgetFunc func(pf ProgressFace) string

// ProgressFace interface
type ProgressFace interface {
	Start(maxSteps ...int)
	Advance(steps ...uint)
	AdvanceTo(step uint)
	Finish()
}

// Progress definition
// Refer:
// 	https://github.com/inhere/php-console/blob/master/src/Utils/ProgressBar.php
type Progress struct {
	// Format string the bar format
	Format string
	// MaxSteps maximal steps.
	MaxSteps uint
	// StepWidth the width for display "{@current}". default is 2
	StepWidth uint8
	// Overwrite prev output. default is True
	Overwrite bool
	// RedrawFreq redraw freq. default is 1
	RedrawFreq uint8
	// Widgets for build the progress bar
	Widgets map[string]WidgetFunc
	// Messages named messages for build progress bar
	// Example:
	// 	{"msg": "downloading ..."}
	// 	"{@percent}% {@msg}" -> "83% downloading ..."
	Messages map[string]string
	// current step value
	step uint
	// mark start status
	started bool
	// binding current progress instance. use for widget handler(p.binding)
	// if you extends this Progress, must setting it.
	binding ProgressFace
	// completed percent. eg: "83.8"
	percent float32
	// mark is first running
	firstRun bool
	// time consumption record
	startedAt  time.Time
	finishedAt time.Time
}

/*************************************************************
 * quick use
 *************************************************************/

// New Progress instance
func New(maxSteps ...int) *Progress {
	var max uint
	if len(maxSteps) > 0 {
		max = uint(maxSteps[0])
	}

	return &Progress{
		Format:    DefFormat,
		MaxSteps:  max,
		StepWidth: 2,
		Overwrite: true,
		// init widgets
		Widgets: make(map[string]WidgetFunc),
		// add a default message
		Messages: map[string]string{"message": ""},
	}
}

// Txt progress bar create.
func Txt(maxSteps ...int) *Progress {
	return New(maxSteps...).Config(func(p *Progress) {
		p.Format = TxtFormat
	})
}

// Full progress bar create.
func Full(maxSteps ...int) *Progress {
	return New(maxSteps...).Config(func(p *Progress) {
		p.Format = FullFormat
	})
}

/*************************************************************
 * config
 *************************************************************/

// Config the progress instance
func (p *Progress) Config(fn func(p *Progress)) *Progress {
	fn(p)
	return p
}

// SetBinding instance
func (p *Progress) SetBinding(binding ProgressFace) {
	p.binding = binding
}

// AddMessage to progress
func (p *Progress) AddMessage(name, message string) {
	p.Messages[name] = message
}

// AddMessages to progress
func (p *Progress) AddMessages(msgMap map[string]string) {
	if p.Messages == nil {
		p.Messages = make(map[string]string)
	}

	for name, message := range msgMap {
		p.Messages[name] = message
	}
}

// AddWidget to progress
func (p *Progress) AddWidget(name string, handler WidgetFunc) {
	if _, ok := p.Widgets[name]; !ok {
		p.Widgets[name] = handler
	}
}

// SetWidget to progress
func (p *Progress) SetWidget(name string, handler WidgetFunc) {
	p.Widgets[name] = handler
}

// AddWidgets to progress
func (p *Progress) AddWidgets(widgets map[string]WidgetFunc) {
	if p.Widgets == nil {
		p.Widgets = make(map[string]WidgetFunc)
	}

	for name, handler := range widgets {
		p.AddWidget(name, handler)
	}
}

/*************************************************************
 * running
 *************************************************************/

// Start the progress bar
func (p *Progress) Start(maxSteps ...int) {
	if p.started {
		panic("Progress bar already started")
	}

	// init
	p.init(maxSteps...)

	// render
	p.Display()
}

func (p *Progress) init(maxSteps ...int) {
	p.step = 0
	p.percent = 0.0
	p.started = true
	p.startedAt = time.Now()

	if p.RedrawFreq == 0 {
		p.RedrawFreq = 1
	}

	if len(maxSteps) > 0 {
		p.MaxSteps = uint(maxSteps[0])
	}

	if p.StepWidth == 0 {
		p.StepWidth = 2

		// use MaxSteps len as StepWidth. eg: MaxSteps=1000 -> StepWidth=4
		if p.MaxSteps > 0 {
			maxStepsLen := len(fmt.Sprint(p.MaxSteps))
			p.StepWidth = uint8(maxStepsLen)
		}
	}

	// load default widgets
	p.AddWidgets(builtinWidgets)
}

// Advance specified step size. default is 1
func (p *Progress) Advance(steps ...uint) {
	p.checkStart()

	var step uint = 1
	if len(steps) > 0 && steps[0] > 0 {
		step = steps[0]
	}

	p.AdvanceTo(p.step + step)
}

// AdvanceTo specified number of steps
func (p *Progress) AdvanceTo(step uint) {
	p.checkStart()

	// check arg
	if p.MaxSteps > 0 && step > p.MaxSteps {
		p.MaxSteps = step
	}

	freq := uint(p.RedrawFreq)
	prevPeriod := int(p.step / freq)
	currPeriod := int(step / freq)

	p.step = step
	if p.MaxSteps > 0 {
		p.percent = float32(p.step) / float32(p.MaxSteps)
	}

	if prevPeriod != currPeriod || p.MaxSteps == step {
		p.Display()
	}
}

// Finish the progress output.
func (p *Progress) Finish() {
	p.checkStart()
	p.finishedAt = time.Now()

	if p.MaxSteps == 0 {
		p.MaxSteps = p.step
	}

	// prevent double 100% output
	if p.step == p.MaxSteps && !p.Overwrite {
		return
	}

	p.AdvanceTo(p.MaxSteps)
	fmt.Println() // new line
}

// Display outputs the current progress string.
func (p *Progress) Display() {
	if p.Format == "" {
		p.Format = DefFormat
	}

	p.render(p.buildLine())
}

// Destroy removes the progress bar from the current line.
//
// This is useful if you wish to write some output while a progress bar is running.
// Call display() to show the progress bar again.
func (p *Progress) Destroy() {
	if p.Overwrite {
		p.render("")
	}
}

/*************************************************************
 * helper methods
 *************************************************************/

// render progress bar to terminal
func (p *Progress) render(text string) {
	if p.Overwrite {
		if p.firstRun { // first run. create new line
			fmt.Println()
			p.firstRun = false
			return
		}

		// \x0D - Move the cursor to the beginning of the line
		// \x1B[2K - Erase(Delete) the line
		fmt.Print("\x0D\x1B[2K")
		fmt.Print(text)
	} else if p.step > 0 {
		fmt.Println(text)
	}
}

func (p *Progress) checkStart() {
	if !p.started {
		panic("Progress bar has not yet been started.")
	}
}

// build widgets form Format string.
func (p *Progress) buildLine() string {
	return widgetMatch.ReplaceAllStringFunc(p.Format, func(s string) string {
		var text string
		// {@current} -> current
		// {@percent:3s} -> percent:3s
		name := strings.Trim(s, "{@}")
		fmtArg := ""

		// percent:3s
		if pos := strings.IndexRune(name, ':'); pos > 0 {
			fmtArg = name[pos+1:]
			name = name[0:pos]
		}

		if handler, ok := p.Widgets[name]; ok {
			if p.binding == nil {
				text = handler(p)
			} else {
				text = handler(p.binding)
			}
		} else if msg, ok := p.Messages[name]; ok {
			text = msg
		} else {
			return s
		}

		if fmtArg != "" { // like {@percent:3s} "7%" -> "  7%"
			text = fmt.Sprintf("%"+fmtArg, text)
		}
		// fmt.Println("info:", arg, name, ", text:", text)
		return text
	})
}

// Handler get widget handler by widget name
func (p *Progress) Handler(name string) WidgetFunc {
	if handler, ok := p.Widgets[name]; ok {
		return handler
	}

	return nil
}

/*************************************************************
 * getter methods
 *************************************************************/

// Percent gets the current percent
func (p *Progress) Percent() float32 {
	return p.percent
}

// Step gets the current step position.
func (p *Progress) Step() uint {
	return p.step
}

// Progress alias of the Step()
func (p *Progress) Progress() uint {
	return p.step
}

// StartedAt time get
func (p *Progress) StartedAt() time.Time {
	return p.startedAt
}

// FinishedAt time get
func (p *Progress) FinishedAt() time.Time {
	return p.finishedAt
}