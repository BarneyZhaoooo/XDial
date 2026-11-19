//go:build !windows

package libbox

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/log"
)

// DataPlaneLogSink 接收 sing-box 数据面 warn 及以上级别的日志行。
//
// sing-box 作为库跑在系统扩展里时，它自己的日志写到 stderr 就丢了；宿主把这个
// 接口接到统一日志，数据面为什么关掉一条连接才查得到。info 及以下不经过这里：
// 每条新流都有几行，只会刷屏。
//
// summary 已去掉地址、域名和凭据，可以按公开字段记录；detail 保留地址和域名、
// 只去掉凭据，宿主应按私有字段记录。level 是 "warn" 或 "error"。
type DataPlaneLogSink interface {
	WriteDataPlaneLog(level string, summary string, detail string)
}

// SetDataPlaneLogSink 设置之后启动的每一代 sing-box 的日志出口。已经在运行的
// 一代不受影响；传 nil 表示之后启动的不再接出日志。
func (l *Libbox) SetDataPlaneLogSink(sink DataPlaneLogSink) {
	l.mu.Lock()
	l.dataPlaneLogSink = sink
	l.mu.Unlock()
}

// attachDataPlaneLog 把 sink 接到一代刚构造好的 sing-box 上。
//
// 不走 box.Options.PlatformLogWriter：那个选项还会顺带打开缓存文件和 Clash API，
// 这里只要日志。
func attachDataPlaneLog(instance *box.Box, sink DataPlaneLogSink) {
	if sink == nil {
		return
	}
	factory, isObservable := instance.LogFactory().(log.ObservableFactory)
	if !isObservable {
		return
	}
	factory.AttachPlatformWriter(newDataPlaneLogWriter(sink, time.Now))
}

const (
	// 突发 60 行，之后每秒 10 行。断网时每条新流都会报一次错，超过的只计数。
	dataPlaneLogBurst       = 60
	dataPlaneLogRefillEvery = 100 * time.Millisecond
)

type dataPlaneLogWriter struct {
	sink DataPlaneLogSink
	now  func() time.Time

	mu         sync.Mutex
	tokens     int
	refilledAt time.Time
	suppressed int
}

var _ log.PlatformWriter = (*dataPlaneLogWriter)(nil)

func newDataPlaneLogWriter(sink DataPlaneLogSink, now func() time.Time) *dataPlaneLogWriter {
	return &dataPlaneLogWriter{
		sink:       sink,
		now:        now,
		tokens:     dataPlaneLogBurst,
		refilledAt: now(),
	}
}

// WriteMessage 会收到所有级别的行（平台出口不受配置的日志级别约束），所以
// 级别判断必须在任何字符串处理之前。
func (w *dataPlaneLogWriter) WriteMessage(level log.Level, message string) {
	if level > log.LevelWarn {
		return
	}
	suppressed, allowed := w.take()
	if !allowed {
		return
	}
	levelName := "error"
	if level == log.LevelWarn {
		levelName = "warn"
	}
	if suppressed > 0 {
		note := "suppressed " + strconv.Itoa(suppressed) + " lines over the rate limit"
		w.sink.WriteDataPlaneLog("warn", note, note)
	}
	detail := dataPlaneLogDetail(message)
	w.sink.WriteDataPlaneLog(levelName, dataPlaneLogSummary(detail), detail)
}

func (w *dataPlaneLogWriter) take() (suppressed int, allowed bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	if refill := int(now.Sub(w.refilledAt) / dataPlaneLogRefillEvery); refill > 0 {
		w.tokens = min(dataPlaneLogBurst, w.tokens+refill)
		w.refilledAt = w.refilledAt.Add(time.Duration(refill) * dataPlaneLogRefillEvery)
	}
	if w.tokens == 0 {
		w.suppressed++
		return 0, false
	}
	w.tokens--
	suppressed = w.suppressed
	w.suppressed = 0
	return suppressed, true
}

const dataPlaneLogRedacted = "<redacted>"

var (
	dataPlaneLogANSI = regexp.MustCompile("\x1b\\[[0-9;]*m")
	// sing-box 平台格式的行首："ERROR[0012] "。级别另有字段，时间由统一日志记。
	dataPlaneLogPrefix = regexp.MustCompile(`^[A-Z]+\[\d+\] `)
	// sing 的 SOCKS / HTTP 入站在认证失败时把用户名和密码原样拼进错误。
	dataPlaneLogCredential = regexp.MustCompile(
		`(?i)\b(user(name)?|pass(word)?|passwd|token|secret|auth(orization)?|key|uuid)\s*[=:]`,
	)
	dataPlaneLogDuration = regexp.MustCompile(`^\d+(\.\d+)?(ns|µs|us|ms|s|m|h)+$`)
	dataPlaneLogOpaque   = regexp.MustCompile(`^[A-Za-z0-9+/_=-]{32,}$`)
)

// dataPlaneLogDetail 去掉颜色、行首和凭据，保留地址与域名。凭据的值可能含空格，
// 没法可靠地只切掉值，所以从第一个凭据字段起整段丢弃。
func dataPlaneLogDetail(message string) string {
	message = dataPlaneLogANSI.ReplaceAllString(message, "")
	message = dataPlaneLogPrefix.ReplaceAllString(message, "")
	message = strings.TrimSpace(message)
	if location := dataPlaneLogCredential.FindStringIndex(message); location != nil {
		message = strings.TrimRight(message[:location[0]], " ,;") + " " + dataPlaneLogRedacted
	}
	return message
}

// dataPlaneLogSummary 在 detail 的基础上再去掉网络标识：地址、端口、域名、URL
// 和长的不透明串。判断按词进行，宁可多遮：被遮掉的内容仍在 detail 里。
func dataPlaneLogSummary(detail string) string {
	words := strings.Split(detail, " ")
	for index, word := range words {
		if word == "" {
			continue
		}
		if dataPlaneLogWordIsIdentifier(word) ||
			dataPlaneLogFollowsLookup(words, index) ||
			dataPlaneLogIsDNSQuestionName(words, index) {
			words[index] = dataPlaneLogRedacted
		}
	}
	return strings.Join(words, " ")
}

func dataPlaneLogWordIsIdentifier(word string) bool {
	core := strings.Trim(word, "[]()<>\"',;")
	core = strings.TrimRight(core, ":.")
	if core == "" || dataPlaneLogDuration.MatchString(core) {
		return false
	}
	return strings.ContainsAny(core, ":@.") ||
		strings.Contains(core, "//") ||
		dataPlaneLogOpaque.MatchString(core)
}

// Go 解析器的错误形如 "lookup wiki on 10.0.0.1:53"，单标签主机名没有点号。
func dataPlaneLogFollowsLookup(words []string, index int) bool {
	return index > 0 && strings.EqualFold(words[index-1], "lookup")
}

// sing-box 的 DNS 错误形如 "exchange failed for wiki. IN A"。
func dataPlaneLogIsDNSQuestionName(words []string, index int) bool {
	return index+1 < len(words) && words[index+1] == "IN" && strings.HasSuffix(words[index], ".")
}
