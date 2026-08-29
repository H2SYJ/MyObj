package service

import (
	"testing"
	"time"
)

// nextScheduleInLocation 需同时兼容存量"HH:mm"（每日一次）与cron表达式
// （支持5段自动补秒位、6段含秒、@daily等别名），并按指定时区计算下次执行时间。
func TestNextScheduleInLocation(t *testing.T) {
	location := time.FixedZone("CST", 8*3600)
	tests := []struct {
		name    string
		value   string
		now     time.Time
		want    time.Time
		wantErr bool
	}{
		// 存量HH:mm格式
		{
			name:  "HH:mm当天未到",
			value: "08:00",
			now:   time.Date(2026, 8, 30, 7, 0, 0, 0, location),
			want:  time.Date(2026, 8, 30, 8, 0, 0, 0, location),
		},
		{
			name:  "HH:mm当天已过顺延一天",
			value: "08:00",
			now:   time.Date(2026, 8, 30, 9, 0, 0, 0, location),
			want:  time.Date(2026, 8, 31, 8, 0, 0, 0, location),
		},
		{
			name:  "HH:mm边界等于当前时刻顺延一天",
			value: "08:00",
			now:   time.Date(2026, 8, 30, 8, 0, 0, 0, location),
			want:  time.Date(2026, 8, 31, 8, 0, 0, 0, location),
		},
		{
			name:  "HH:mm首位可不补零",
			value: "8:05",
			now:   time.Date(2026, 8, 30, 7, 0, 0, 0, location),
			want:  time.Date(2026, 8, 30, 8, 5, 0, 0, location),
		},
		// 5段表达式自动补秒位
		{
			name:  "5段每日8点等价于08:00",
			value: "0 8 * * *",
			now:   time.Date(2026, 8, 30, 7, 30, 0, 0, location),
			want:  time.Date(2026, 8, 30, 8, 0, 0, 0, location),
		},
		{
			name:  "5段每2分钟步进",
			value: "*/2 * * * *",
			now:   time.Date(2026, 8, 30, 7, 30, 25, 0, location),
			want:  time.Date(2026, 8, 30, 7, 32, 0, 0, location),
		},
		// 6段含秒表达式
		{
			name:  "6段每日8点30分15秒",
			value: "15 30 8 * * *",
			now:   time.Date(2026, 8, 30, 7, 30, 0, 0, location),
			want:  time.Date(2026, 8, 30, 8, 30, 15, 0, location),
		},
		{
			name:  "6段每30秒步进",
			value: "*/30 * * * * *",
			now:   time.Date(2026, 8, 30, 7, 30, 10, 0, location),
			want:  time.Date(2026, 8, 30, 7, 30, 30, 0, location),
		},
		{
			name:  "6段指定秒级瞬间",
			value: "15 0 8 * * *",
			now:   time.Date(2026, 8, 30, 7, 59, 0, 0, location),
			want:  time.Date(2026, 8, 30, 8, 0, 15, 0, location),
		},
		// @别名（cron.Descriptor）
		{
			name:  "@daily每日零点",
			value: "@daily",
			now:   time.Date(2026, 8, 30, 15, 0, 0, 0, location),
			want:  time.Date(2026, 8, 31, 0, 0, 0, 0, location),
		},
		{
			name:  "@hourly下个整点",
			value: "@hourly",
			now:   time.Date(2026, 8, 30, 15, 30, 10, 0, location),
			want:  time.Date(2026, 8, 30, 16, 0, 0, 0, location),
		},
		// 非法输入
		{name: "非法时刻25点", value: "25:00", now: time.Date(2026, 8, 30, 7, 0, 0, 0, location), wantErr: true},
		{name: "非法cron小时25", value: "0 25 * * *", now: time.Date(2026, 8, 30, 7, 0, 0, 0, location), wantErr: true},
		{name: "3段表达式", value: "a b c", now: time.Date(2026, 8, 30, 7, 0, 0, 0, location), wantErr: true},
		{name: "空字符串", value: "", now: time.Date(2026, 8, 30, 7, 0, 0, 0, location), wantErr: true},
		{name: "纯空白", value: "   ", now: time.Date(2026, 8, 30, 7, 0, 0, 0, location), wantErr: true},
		{
			name:    "7段表达式",
			value:   "0 0 8 * * * *",
			now:     time.Date(2026, 8, 30, 7, 0, 0, 0, location),
			wantErr: true,
		},
		{name: "未知别名", value: "@nonexistent", now: time.Date(2026, 8, 30, 7, 0, 0, 0, location), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := nextScheduleInLocation(tt.value, tt.now, location)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际得到%v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Fatalf("下次执行时间=%v，期望%v", got, tt.want)
			}
		})
	}
}

// 6段含秒表达式中，秒级粒度依赖now的秒数，需验证秒级推进正确。
func TestNextScheduleInLocationSecondPrecision(t *testing.T) {
	location := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 8, 30, 7, 30, 0, 0, location)
	next, err := nextScheduleInLocation("*/10 * * * * *", now, location)
	if err != nil {
		t.Fatal(err)
	}
	if next.Second() != 10 || !next.Equal(time.Date(2026, 8, 30, 7, 30, 10, 0, location)) {
		t.Fatalf("每10秒表达式下次时间=%v，期望07:30:10", next)
	}
}

// 同一表达式在不同时区应得到不同的下次执行时刻。
func TestNextScheduleInLocationTimezone(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skipf("无法加载时区Asia/Shanghai: %v", err)
	}
	utc := time.UTC
	now := time.Date(2026, 8, 30, 0, 0, 0, 0, utc)
	nextShanghai, err := nextScheduleInLocation("0 8 * * *", now, shanghai)
	if err != nil {
		t.Fatal(err)
	}
	nextUTC, err := nextScheduleInLocation("0 8 * * *", now, utc)
	if err != nil {
		t.Fatal(err)
	}
	if nextShanghai.Equal(nextUTC) {
		t.Fatalf("上海与UTC时区的下次执行时间不应相同: %v", nextShanghai)
	}
	if !nextUTC.Equal(time.Date(2026, 8, 30, 8, 0, 0, 0, utc)) {
		t.Fatalf("UTC下次执行时间=%v，期望2026-08-30 08:00 UTC", nextUTC)
	}
}

// 兼容性：函数级包装nextSchedule应透传cron解析结果。
func TestNextScheduleWrapper(t *testing.T) {
	now := time.Date(2026, 8, 30, 7, 0, 0, 0, time.Local)
	next, err := nextSchedule("*/15 * * * * *", now)
	if err != nil {
		t.Fatal(err)
	}
	if next.Sub(now) != 15*time.Second {
		t.Fatalf("每15秒表达式距下次执行=%v，期望15秒", next.Sub(now))
	}
}
