package api

// 磁盘与挂载管理：块设备列表、挂载/卸载、开机自动挂载（fstab）。

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"time"

	"nanopanel/internal/sysinfo"
)

var (
	devicePathRe   = regexp.MustCompile(`^/dev/[A-Za-z0-9/._-]{1,64}$`)
	mountPointRe   = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,120}$`)
	mountableFSTes = map[string]bool{ // 可手动挂载的文件系统
		"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true,
		"f2fs": true, "vfat": true, "exfat": true, "exfat-fuse": true,
		"ntfs": true, "ntfs3": true, "hfsplus": true,
	}
	// 展示用文件系统：挂载点列表里保留哪些
	visibleFSTes = map[string]bool{
		"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true,
		"f2fs": true, "vfat": true, "exfat": true, "exfat-fuse": true,
		"ntfs": true, "ntfs3": true, "hfsplus": true, "9p": true,
		"drvfs": true, "nfs": true, "nfs4": true, "cifs": true, "fuseblk": true,
	}
)

// DiskUsage 一个挂载点的用量快照。
type DiskUsage struct {
	TotalMB int     `json:"total_mb"`
	AvailMB int     `json:"avail_mb"`
	UsedMB  int     `json:"used_mb"`
	UsedPct float64 `json:"used_pct"`
}

// DiskMount 单个挂载点。
type DiskMount struct {
	Mountpoint string     `json:"mountpoint"`
	Usage      *DiskUsage `json:"usage,omitempty"`
}

// DiskEntry 块设备（整盘或分区）。
type DiskEntry struct {
	Name      string      `json:"name"`
	Path      string      `json:"path"`
	Size      string      `json:"size"` // lsblk 的人类可读容量
	Type      string      `json:"type"` // disk / part
	FSType    string      `json:"fstype,omitempty"`
	Model     string      `json:"model,omitempty"`
	Removable bool        `json:"removable"`
	Mounts    []DiskMount `json:"mounts,omitempty"`
	Usage     *DiskUsage  `json:"usage,omitempty"` // 第一个挂载点的用量
	Mountable bool        `json:"mountable"`
}

// StandaloneMount 不属于任何 lsblk 块设备的挂载项（WSL 的 9p/drvfs、NFS 等）。
type StandaloneMount struct {
	Device     string     `json:"device"`
	Mountpoint string     `json:"mountpoint"`
	FSType     string     `json:"fstype"`
	ReadOnly   bool       `json:"read_only"`
	Usage      *DiskUsage `json:"usage,omitempty"`
}

type lsblkDevice struct {
	Name        string        `json:"name"`
	Path        string        `json:"path"`
	Size        string        `json:"size"`
	Type        string        `json:"type"`
	FSType      string        `json:"fstype"`
	Model       string        `json:"model"`
	RM          bool          `json:"rm"`
	Mountpoints []interface{} `json:"mountpoints"`
	Children    []lsblkDevice `json:"children,omitempty"`
}

type lsblkOutput struct {
	Blockdevices []lsblkDevice `json:"blockdevices"`
}

// usageOf 计算挂载点的用量。
func usageOf(mountpoint string) *DiskUsage {
	total, avail := sysinfo.DiskUsage(mountpoint)
	if total == 0 {
		return nil
	}
	used := total - avail
	pct := 0.0
	if total > 0 {
		pct = float64(used) / float64(total) * 100
		pct = float64(int(pct*10)) / 10
	}
	return &DiskUsage{
		TotalMB: int(total / 1024 / 1024),
		AvailMB: int(avail / 1024 / 1024),
		UsedMB:  int(used / 1024 / 1024),
		UsedPct: pct,
	}
}

// lsblkMounts 清洗 mountpoints 字段（lsblk 对未挂载项输出 null，swap 显示为 [SWAP]）。
func lsblkMounts(d *lsblkDevice) []string {
	out := make([]string, 0, len(d.Mountpoints))
	for _, m := range d.Mountpoints {
		if s, ok := m.(string); ok && s != "" && s != "[SWAP]" {
			out = append(out, s)
		}
	}
	return out
}

// readMounts 读取 /proc/self/mounts 中的可见文件系统挂载。
func readMounts() []StandaloneMount {
	data, err := os.ReadFile("/proc/self/mounts")
	if err != nil {
		return nil
	}
	out := make([]StandaloneMount, 0)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		device, mountpoint, fstype := fields[0], fields[1], fields[2]
		if !visibleFSTes[fstype] {
			continue
		}
		// /proc/self/mounts 使用八进制转义：\040 空格、\011 制表符、\134 反斜杠、\012 换行
		mountpoint = strings.NewReplacer(
			`\040`, " ", `\011`, "\t", `\134`, `\`, `\012`, "\n",
		).Replace(mountpoint)
		opts := fields[3]
		out = append(out, StandaloneMount{
			Device:     device,
			Mountpoint: mountpoint,
			FSType:     fstype,
			ReadOnly:   strings.Contains(opts, "ro"),
		})
	}
	return out
}

// ListDisks 返回块设备与挂载信息。
func (s *Server) ListDisks(w http.ResponseWriter, r *http.Request) {
	devices, knownDevices := s.listBlockDevices(r.Context())
	allMounts := readMounts()

	// 已被 lsblk 呈现的挂载不再重复出现在独立列表。
	standalone := make([]StandaloneMount, 0, len(allMounts))
	for _, m := range allMounts {
		if !knownDevices[m.Device] {
			m.Usage = usageOf(m.Mountpoint)
			standalone = append(standalone, m)
		}
	}
	for i := range standalone {
		if standalone[i].Usage == nil {
			standalone[i].Usage = usageOf(standalone[i].Mountpoint)
		}
	}

	ok(w, map[string]interface{}{
		"devices":    devices,
		"mounts":     standalone,
		"lsblk":      len(devices) > 0,
		"fstab_path": "/etc/fstab",
	})
}

// listBlockDevices 解析 lsblk 输出。
// 有子分区的整盘不单独成卡（分区会各自成卡），无子分区的整盘直接成卡。
func (s *Server) listBlockDevices(ctx context.Context) ([]DiskEntry, map[string]bool) {
	known := map[string]bool{}
	c, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, "lsblk", "-J",
		"-o", "NAME,PATH,SIZE,TYPE,FSTYPE,MOUNTPOINTS,MODEL,RM").Output()
	if err != nil {
		return []DiskEntry{}, known
	}
	var parsed lsblkOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return []DiskEntry{}, known
	}

	mountsByDevice := map[string][]DiskMount{}
	for _, m := range readMounts() {
		mountsByDevice[m.Device] = append(mountsByDevice[m.Device],
			DiskMount{Mountpoint: m.Mountpoint})
	}

	devices := make([]DiskEntry, 0)
	var walk func(list []lsblkDevice)
	walk = func(list []lsblkDevice) {
		for i := range list {
			d := &list[i]
			if d.Type == "loop" || d.Type == "rom" {
				continue
			}
			known[d.Path] = true
			hasChildren := len(d.Children) > 0
			if d.Type == "disk" && hasChildren {
				walk(d.Children)
				continue
			}
			if d.Type != "disk" && d.Type != "part" {
				continue
			}
			pts := lsblkMounts(d)
			if len(pts) == 0 {
				// lsblk 未列出时回退查 /proc/self/mounts
				if mm := mountsByDevice[d.Path]; len(mm) > 0 {
					pts = make([]string, 0, len(mm))
					for _, x := range mm {
						pts = append(pts, x.Mountpoint)
					}
				}
			}
			mounts := make([]DiskMount, 0, len(pts))
			for _, p := range pts {
				mounts = append(mounts, DiskMount{Mountpoint: p, Usage: usageOf(p)})
			}
			entry := DiskEntry{
				Name:      d.Name,
				Path:      d.Path,
				Size:      d.Size,
				Type:      d.Type,
				FSType:    d.FSType,
				Model:     strings.TrimSpace(d.Model),
				Removable: d.RM,
				Mounts:    mounts,
				Mountable: mountableFSTes[d.FSType] && len(mounts) == 0,
			}
			if len(mounts) > 0 {
				entry.Usage = mounts[0].Usage
			}
			known[d.Path] = true
			devices = append(devices, entry)
		}
	}
	walk(parsed.Blockdevices)
	return devices, known
}

type mountReq struct {
	Device     string `json:"device"`
	Mountpoint string `json:"mountpoint"`
	Fstab      bool   `json:"fstab"`
}

// mountPrefixWhitelist 只允许挂载到数据目录下，防止把磁盘挂到 /etc、/usr 等系统目录上遮蔽系统文件。
var mountPrefixWhitelist = []string{"/mnt/", "/media/", "/opt/", "/srv/", "/home/", "/data/"}

// validMountPoint 校验并规范化挂载点路径。
func validMountPoint(p string) (string, error) {
	if !mountPointRe.MatchString(p) {
		return "", fmt.Errorf("挂载点路径不合法")
	}
	// 始终以 POSIX 语义清洗：挂载点是 Linux 路径，不能受构建平台影响。
	clean := path.Clean("/" + p)
	if strings.Contains(clean, "..") {
		return "", fmt.Errorf("挂载点路径不合法")
	}
	if len(clean) > 160 {
		return "", fmt.Errorf("挂载点路径过长")
	}
	allowed := false
	for _, prefix := range mountPrefixWhitelist {
		if clean == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(clean, prefix) {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("挂载点需位于 /mnt、/media、/opt、/srv、/home 或 /data 之下")
	}
	return clean, nil
}

// DiskMount 挂载块设备，可选写入 fstab 开机自动挂载。
func (s *Server) DiskMount(w http.ResponseWriter, r *http.Request) {
	var req mountReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if !devicePathRe.MatchString(req.Device) {
		fail(w, http.StatusBadRequest, "设备路径不合法，需形如 /dev/sdb1")
		return
	}
	mountpoint, err := validMountPoint(req.Mountpoint)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if st, err := os.Stat(req.Device); err != nil || st.Mode()&os.ModeDevice == 0 {
		fail(w, http.StatusBadRequest, "设备不存在或不是块设备: "+req.Device)
		return
	}
	if mp := findMount(req.Device); mp != "" {
		fail(w, http.StatusConflict, "该设备已挂载在 "+mp)
		return
	}

	if err := os.MkdirAll(mountpoint, 0755); err != nil {
		fail(w, http.StatusInternalServerError, "创建挂载目录失败: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 60*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "mount", req.Device, mountpoint).CombinedOutput(); err != nil {
		s.audit(r, "disk", "挂载磁盘失败", req.Device+" -> "+mountpoint)
		fail(w, http.StatusInternalServerError,
			fmt.Sprintf("挂载失败: %v\n%s", err, strings.TrimSpace(string(out))))
		return
	}

	fstabNote := ""
	if req.Fstab {
		note, err := appendFstab(ctx, req.Device, mountpoint)
		if err != nil {
			fstabNote = "（已挂载，但写入 fstab 失败: " + err.Error() + "）"
		} else if note {
			fstabNote = "，已写入 /etc/fstab 开机自动挂载"
		} else {
			fstabNote = "，fstab 已有该设备记录"
		}
	}

	s.audit(r, "disk", "挂载磁盘", req.Device+" -> "+mountpoint)
	s.notifyAsync("disk", "磁盘已挂载", req.Device+" -> "+mountpoint+fstabNote)
	ok(w, map[string]string{"msg": "已挂载到 " + mountpoint + fstabNote, "mountpoint": mountpoint})
}

type umountReq struct {
	Target string `json:"target"` // 挂载点或设备路径
}

// DiskUmount 卸载指定挂载点或设备。
func (s *Server) DiskUmount(w http.ResponseWriter, r *http.Request) {
	var req umountReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	target := strings.TrimSpace(req.Target)
	if !devicePathRe.MatchString(target) {
		if mp, err := validMountPoint(target); err == nil {
			target = mp
		} else {
			fail(w, http.StatusBadRequest, "目标路径不合法")
			return
		}
	}
	if findMount(target) == "" && !isDevice(target) {
		fail(w, http.StatusNotFound, "目标未处于挂载状态")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 60*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "umount", target).CombinedOutput(); err != nil {
		s.audit(r, "disk", "卸载磁盘失败", target)
		fail(w, http.StatusInternalServerError,
			fmt.Sprintf("卸载失败: %v\n%s", err, strings.TrimSpace(string(out))))
		return
	}
	s.audit(r, "disk", "卸载磁盘", target)
	s.notifyAsync("disk", "磁盘已卸载", target)
	ok(w, map[string]string{"msg": "已卸载 " + target})
}

// findMount 在 /proc/self/mounts 中查找设备或挂载点，返回其挂载点。
func findMount(target string) string {
	for _, m := range readMounts() {
		if m.Device == target || m.Mountpoint == target {
			return m.Mountpoint
		}
	}
	return ""
}

func isDevice(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode()&os.ModeDevice != 0
}

// appendFstab 幂等地把设备写入 /etc/fstab。返回是否新写入。
func appendFstab(ctx context.Context, device, mountpoint string) (bool, error) {
	data, err := os.ReadFile("/etc/fstab")
	if err != nil {
		return false, err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && (fields[0] == device || fields[1] == mountpoint) {
			return false, nil
		}
	}
	uuid := ""
	if out, err := exec.CommandContext(ctx, "blkid", "-s", "UUID", "-o", "value", device).Output(); err == nil {
		uuid = strings.TrimSpace(string(out))
	}
	spec := device
	if uuid != "" {
		spec = "UUID=" + uuid
	}
	entry := fmt.Sprintf("%s %s auto defaults,nofail 0 2\n", spec, mountpoint)
	f, err := os.OpenFile("/etc/fstab", os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	if _, err := f.WriteString(entry); err != nil {
		return false, err
	}
	return true, nil
}
