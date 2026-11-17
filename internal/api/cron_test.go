package api

import (
	"testing"
	"time"
)

func freshCronState() *cronState {
	return &cronState{
		lastTrigger: make(map[string]time.Time),
		lastDaily:   make(map[string]string),
	}
}

func at(hour, minute int) time.Time {
	return time.Date(2026, 11, 17, hour, minute, 0, 0, time.Local)
}

// 回归：面板在 daily 计划时刻"之前"启动，当天到点必须触发。
// 旧逻辑把 lastDaily 一律设为启动当天，导致当天任务被永久跳过。
func TestDailyFiresWhenStartedBeforeSchedule(t *testing.T) {
	state := freshCronState()
	task := CronTask{ID: "d1", Schedule: "daily:03:30"}
	primeCronBaseline(state, task, at(2, 0)) // 02:00 启动，早于 03:30

	if got := cronDue(state, task, at(3, 35)); !got {
		t.Fatal("daily：计划时刻前启动，到点应触发，实际未触发")
	}
	// 触发后同一天不得重复。
	if got := cronDue(state, task, at(4, 0)); got {
		t.Fatal("daily：同一天不应重复触发")
	}
	// 跨天到点应再次触发。
	next := at(3, 31).AddDate(0, 0, 1)
	if got := cronDue(state, task, next); !got {
		t.Fatal("daily：次日到点应触发，实际未触发")
	}
}

// 面板在 daily 计划时刻"之后"启动，当天不补跑（保持既有语义）。
func TestDailyNoCatchUpWhenStartedAfterSchedule(t *testing.T) {
	state := freshCronState()
	task := CronTask{ID: "d2", Schedule: "daily:03:30"}
	primeCronBaseline(state, task, at(4, 0)) // 04:00 启动，已过 03:30

	if got := cronDue(state, task, at(4, 0)); got {
		t.Fatal("daily：计划时刻后启动不应立即补跑")
	}
}

// hourly：启动时刻早于本小时计划分钟时，到点应触发。
func TestHourlyFiresWhenStartedBeforeMinute(t *testing.T) {
	state := freshCronState()
	task := CronTask{ID: "h1", Schedule: "hourly:15"}
	primeCronBaseline(state, task, at(10, 10)) // 10:10 启动，早于 10:15

	if got := cronDue(state, task, at(10, 15)); !got {
		t.Fatal("hourly：计划分钟前启动，到点应触发")
	}
	if got := cronDue(state, task, at(10, 16)); got {
		t.Fatal("hourly：同一小时不应重复触发")
	}
	// 跨小时到点应触发。
	if got := cronDue(state, task, at(11, 15)); !got {
		t.Fatal("hourly：下一小时到点应触发")
	}
}

// hourly：启动时刻已过本小时计划分钟，本小时不补跑，下小时正常触发。
func TestHourlyNoCatchUpWithinStartedHour(t *testing.T) {
	state := freshCronState()
	task := CronTask{ID: "h2", Schedule: "hourly:15"}
	primeCronBaseline(state, task, at(10, 20)) // 10:20 启动，已过 10:15

	if got := cronDue(state, task, at(10, 45)); got {
		t.Fatal("hourly：本小时已过计划分钟不应补跑")
	}
	if got := cronDue(state, task, at(11, 15)); !got {
		t.Fatal("hourly：下一小时到点应触发")
	}
}

// every：间隔内不触发，达到间隔后触发并重置基准。
func TestEveryInterval(t *testing.T) {
	state := freshCronState()
	task := CronTask{ID: "e1", Schedule: "every:30m"}
	primeCronBaseline(state, task, at(10, 0))

	if got := cronDue(state, task, at(10, 20)); got {
		t.Fatal("every：间隔未到不应触发")
	}
	if got := cronDue(state, task, at(10, 31)); !got {
		t.Fatal("every：达到间隔应触发")
	}
	if got := cronDue(state, task, at(11, 0)); got {
		t.Fatal("every：触发后间隔未到不应重复")
	}
}

// 未有基准的任务（如运行期新建）首次检查只建立基准不触发，之后按周期判定。
func TestNewTaskPrimesBeforeFirstFire(t *testing.T) {
	state := freshCronState()
	task := CronTask{ID: "n1", Schedule: "every:30m"}
	now := at(10, 0)

	if got := cronDue(state, task, now); got {
		t.Fatal("新建任务首轮不应立即执行")
	}
	if got := cronDue(state, task, at(10, 31)); !got {
		t.Fatal("新建任务建立基准后应按周期触发")
	}
}

func TestValidSchedule(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"every:30m", true},
		{"every:1440m", true},
		{"every:4m", false},
		{"every:30", false},
		{"hourly:0", true},
		{"hourly:59", true},
		{"hourly:60", false},
		{"daily:03:30", true},
		{"daily:23:59", true},
		{"daily:24:00", false},
		{"weekly:1", false},
		{"", false},
	}
	for _, c := range cases {
		if got := validSchedule(c.in); got != c.want {
			t.Errorf("validSchedule(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
