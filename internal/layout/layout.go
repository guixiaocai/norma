// SPDX-License-Identifier: MIT
package layout

import (
	"debug/elf"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/cilium/ebpf/btf"
	"golang.org/x/sys/unix"
)

const DirectFile uint32 = 0xffffffff

type Offsets struct {
	RqAddr       uint32 `json:"rq_addr"`
	RqDaddr      uint32 `json:"rq_daddr"`
	RqVers       uint32 `json:"rq_vers"`
	RqProt       uint32 `json:"rq_prot"`
	RqUID        uint32 `json:"rq_uid"`
	RqGID        uint32 `json:"rq_gid"`
	FileInode    uint32 `json:"file_inode"`
	FileDentry   uint32 `json:"file_dentry"`
	InodeIno     uint32 `json:"inode_ino"`
	InodeSB      uint32 `json:"inode_sb"`
	SBDev        uint32 `json:"sb_dev"`
	DentryParent uint32 `json:"dentry_parent"`
	DentryName   uint32 `json:"dentry_name"`
}
type Hook struct {
	Symbol      string `json:"symbol"`
	RqArg       uint32 `json:"rq_arg"`
	FileArg     uint32 `json:"file_arg"`
	CountArg    uint32 `json:"count_arg"`
	CountBytes  uint32 `json:"count_bytes"`
	FileWrapper uint32 `json:"file_wrapper"`
}
type Layout struct {
	Schema  int     `json:"schema"`
	Kernel  string  `json:"kernel"` // Identity of layout data, not a software version restriction.
	Arch    string  `json:"arch"`
	Source  string  `json:"source"`
	Offsets Offsets `json:"offsets"`
	Hooks   [3]Hook `json:"hooks"`
}

func Kernel() (string, error) {
	var u unix.Utsname
	if err := unix.Uname(&u); err != nil {
		return "", err
	}
	return unix.ByteSliceToString(u.Release[:]), nil
}
func Resolve(path string) (Layout, error) {
	kernel, err := Kernel()
	if err != nil {
		return Layout{}, err
	}
	if runtime.GOARCH != "amd64" {
		return Layout{}, fmt.Errorf("this release supplies x86_64 probes; architecture %s needs a register ABI adapter", runtime.GOARCH)
	}
	if path != "" {
		return read(path, kernel)
	}
	if l, e := fromBTF(kernel); e == nil {
		return l, nil
	}
	for _, dir := range []string{"/var/cache/norma", "/var/cache/nfsmon"} {
		cached := filepath.Join(dir, "layout-"+kernel+".json")
		if l, e := read(cached, kernel); e == nil {
			return l, nil
		}
	}
	return Layout{}, fmt.Errorf("no usable NFSD layout for %s; provide a compatible layout with --layout or expose the required NFSD BTF metadata (see docs/compatibility.md)", kernel)
}
func read(path, kernel string) (Layout, error) {
	var l Layout
	b, err := os.ReadFile(path)
	if err != nil {
		return l, err
	}
	if err = json.Unmarshal(b, &l); err != nil {
		return l, err
	}
	if l.Kernel != kernel || l.Arch != runtime.GOARCH {
		return l, fmt.Errorf("layout %s describes %s/%s; running %s/%s; regenerate layout data", path, l.Kernel, l.Arch, kernel, runtime.GOARCH)
	}
	return l, l.Validate()
}
func (l Layout) Validate() error {
	if l.Schema != 1 {
		return fmt.Errorf("unsupported layout schema %d", l.Schema)
	}
	for _, h := range l.Hooks {
		if h.Symbol == "" || !safeSymbol(h.Symbol) {
			return fmt.Errorf("invalid probe symbol %q", h.Symbol)
		}
		if h.RqArg < 1 || h.RqArg > 8 || h.FileArg < 1 || h.FileArg > 8 || h.CountArg < 1 || h.CountArg > 8 {
			return fmt.Errorf("unsupported calling convention for %s", h.Symbol)
		}
		if h.CountBytes != 4 && h.CountBytes != 8 {
			return fmt.Errorf("unsupported completed count width for %s", h.Symbol)
		}
		if h.FileWrapper != DirectFile && h.FileWrapper > 65535 {
			return fmt.Errorf("invalid wrapper offset")
		}
	}
	o := l.Offsets
	for _, v := range []uint32{o.RqAddr, o.RqDaddr, o.RqVers, o.RqProt, o.RqUID, o.RqGID, o.FileInode, o.FileDentry, o.InodeIno, o.InodeSB, o.SBDev, o.DentryParent, o.DentryName} {
		if v > 65535 {
			return fmt.Errorf("invalid kernel field offset %d", v)
		}
	}
	return nil
}
func safeSymbol(s string) bool {
	for _, c := range s {
		if !(c == '_' || c == '.' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}

type specs []*btf.Spec

func (ss specs) named(name string) (btf.Type, error) {
	for _, s := range ss {
		if t, err := s.AnyTypeByName(name); err == nil {
			return t, nil
		}
	}
	return nil, fmt.Errorf("BTF type %s unavailable", name)
}
func member(t btf.Type, path []string) (uint32, error) {
	t = btf.UnderlyingType(t)
	var members []btf.Member
	switch x := t.(type) {
	case *btf.Struct:
		members = x.Members
	case *btf.Union:
		members = x.Members
	default:
		return 0, fmt.Errorf("%s is not a composite", t.TypeName())
	}
	for _, m := range members {
		if m.Name == path[0] && m.BitfieldSize == 0 {
			if len(path) == 1 {
				return m.Offset.Bytes(), nil
			}
			rest, err := member(m.Type, path[1:])
			return m.Offset.Bytes() + rest, err
		}
	}
	return 0, fmt.Errorf("field %s.%s unavailable", t.TypeName(), strings.Join(path, "."))
}
func (ss specs) offset(typ, path string) (uint32, error) {
	t, err := ss.named(typ)
	if err != nil {
		return 0, err
	}
	return member(t, strings.Split(path, "."))
}
func fromBTF(kernel string) (Layout, error) {
	ss := specs{}
	for _, mod := range []string{"nfsd", "sunrpc"} {
		if s, err := btf.LoadKernelModuleSpec(mod); err == nil {
			ss = append(ss, s)
		}
	}
	if s, err := btf.LoadKernelSpec(); err == nil {
		ss = append(ss, s)
	}
	l := Layout{Schema: 1, Kernel: kernel, Arch: runtime.GOARCH, Source: "runtime BTF types and function prototypes"}
	fields := []struct {
		out       *uint32
		typ, path string
	}{
		{&l.Offsets.RqAddr, "svc_rqst", "rq_addr"}, {&l.Offsets.RqDaddr, "svc_rqst", "rq_daddr"},
		{&l.Offsets.RqVers, "svc_rqst", "rq_vers"}, {&l.Offsets.RqProt, "svc_rqst", "rq_prot"},
		{&l.Offsets.RqUID, "svc_rqst", "rq_cred.cr_uid.val"}, {&l.Offsets.RqGID, "svc_rqst", "rq_cred.cr_gid.val"},
		{&l.Offsets.FileInode, "file", "f_inode"}, {&l.Offsets.FileDentry, "file", "f_path.dentry"},
		{&l.Offsets.InodeIno, "inode", "i_ino"}, {&l.Offsets.InodeSB, "inode", "i_sb"},
		{&l.Offsets.SBDev, "super_block", "s_dev"}, {&l.Offsets.DentryParent, "dentry", "d_parent"}, {&l.Offsets.DentryName, "dentry", "d_name.name"},
	}
	for _, f := range fields {
		v, e := ss.offset(f.typ, f.path)
		if e != nil {
			return l, e
		}
		*f.out = v
	}
	for i, names := range [][]string{{"nfsd_readv", "nfsd_iter_read"}, {"nfsd_splice_read"}, {"nfsd_vfs_write"}} {
		var last error
		for _, n := range names {
			h, e := ss.hook(n)
			if e == nil {
				l.Hooks[i] = h
				last = nil
				break
			}
			last = e
		}
		if last != nil {
			return l, last
		}
	}
	return l, l.Validate()
}
func (ss specs) hook(name string) (Hook, error) {
	h := Hook{Symbol: name, FileWrapper: DirectFile}
	var f *btf.Func
	for _, s := range ss {
		if s.TypeByName(name, &f) == nil {
			break
		}
	}
	if f == nil {
		return h, fmt.Errorf("missing BTF prototype %s", name)
	}
	p, ok := btf.UnderlyingType(f.Type).(*btf.FuncProto)
	if !ok {
		return h, fmt.Errorf("unexpected prototype %s", name)
	}
	// Only __be32 NFS-status return functions are supported. Signed ssize_t paths
	// or scalar requested-count arguments require a separate collector profile.
	ret, ok := btf.UnderlyingType(p.Return).(*btf.Int)
	if !ok || ret.Size != 4 || ret.Encoding&btf.Signed != 0 {
		return h, fmt.Errorf("unsupported return semantics in %s", name)
	}
	for i, a := range p.Params {
		ptr, ok := btf.UnderlyingType(a.Type).(*btf.Pointer)
		if !ok {
			continue
		}
		elem := btf.UnderlyingType(ptr.Target)
		if x, ok := elem.(*btf.Struct); ok {
			switch x.Name {
			case "svc_rqst":
				h.RqArg = uint32(i + 1)
			case "file":
				h.FileArg = uint32(i + 1)
			case "nfsd_file":
				h.FileArg = uint32(i + 1)
				off, e := member(x, []string{"nf_file"})
				if e != nil {
					return h, e
				}
				h.FileWrapper = off
			}
		}
		if a.Name == "count" || a.Name == "cnt" {
			x, ok := elem.(*btf.Int)
			if ok && x.Encoding&btf.Signed == 0 {
				h.CountArg = uint32(i + 1)
				h.CountBytes = x.Size
			}
		}
	}
	if h.RqArg == 0 || h.FileArg == 0 || h.CountArg == 0 {
		return h, fmt.Errorf("unsupported parameter semantics in %s", name)
	}
	return h, nil
}

// ExportObject reads constant offsetof data from an unlinked, never loaded object.
func ExportObject(path, kernel string) ([]byte, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	symbols, err := f.Symbols()
	if err != nil {
		return nil, err
	}
	var values []byte
	for _, s := range symbols {
		if s.Name == "nfsmon_offsets" {
			section := f.Sections[int(s.Section)]
			data, e := section.Data()
			if e != nil {
				return nil, e
			}
			off := s.Value - section.Addr
			if s.Size != 52 || off+52 > uint64(len(data)) {
				return nil, fmt.Errorf("invalid offset object")
			}
			values = data[off : off+52]
			break
		}
	}
	if len(values) != 52 || f.Machine != elf.EM_X86_64 || f.ByteOrder != binary.LittleEndian {
		return nil, fmt.Errorf("missing x86_64 offset data")
	}
	l := Layout{Schema: 1, Kernel: kernel, Arch: "amd64", Source: "matching kernel header offsets; direct-file NFSD profile"}
	ptrs := []*uint32{&l.Offsets.RqAddr, &l.Offsets.RqDaddr, &l.Offsets.RqVers, &l.Offsets.RqProt, &l.Offsets.RqUID, &l.Offsets.RqGID, &l.Offsets.FileInode, &l.Offsets.FileDentry, &l.Offsets.InodeIno, &l.Offsets.InodeSB, &l.Offsets.SBDev, &l.Offsets.DentryParent, &l.Offsets.DentryName}
	for i, p := range ptrs {
		*p = binary.LittleEndian.Uint32(values[i*4:])
	}
	l.Hooks = [3]Hook{{"nfsd_readv", 1, 3, 7, 8, DirectFile}, {"nfsd_splice_read", 1, 3, 5, 8, DirectFile}, {"nfsd_vfs_write", 1, 3, 7, 8, DirectFile}}
	if err = l.Validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(l, "", "  ")
}
