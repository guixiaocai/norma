// SPDX-License-Identifier: BSD-2-Clause
// NFSD VFS completion probes derived from the FrauBSD nfsdtop adaptation.
package trace

import (
	"fmt"
	"nfsmon/internal/layout"
	"strings"
	"time"
)

// Source text uses only numeric field offsets, so bpftrace needs no kernel
// header includes and no handcrafted NFSv4 structures. sarg0 is x86_64 arg 7.
func arg(n uint32) string {
	if n <= 6 {
		return fmt.Sprintf("arg%d", n-1)
	}
	return fmt.Sprintf("sarg%d", n-7)
}
func Script(l layout.Layout, proto uint32, group string, interval time.Duration) string {
	var b strings.Builder
	b.WriteString("/* SPDX-License-Identifier: BSD-2-Clause */\nstruct nfsmon_ipv6 { unsigned char bytes[16]; };\n")
	for i, h := range l.Hooks {
		write := 0
		if i == 2 {
			write = 1
		}
		fmt.Fprintf(&b, "kprobe:%s\n{\n  $rq=(uint64)%s; $file=(uint64)%s; $countp=(uint64)%s;\n", h.Symbol, arg(h.RqArg), arg(h.FileArg), arg(h.CountArg))
		o := l.Offsets
		fmt.Fprintf(&b, "  $proto=*(uint32 *)($rq+%d); $vers=*(uint32 *)($rq+%d); $family=*(uint16 *)($rq+%d); $sf=*(uint16 *)($rq+%d);\n", o.RqProt, o.RqVers, o.RqAddr, o.RqDaddr)
		fmt.Fprintf(&b, "  if ($rq && $file && $countp && (%d==0 || $proto==%d) && ($vers==3 || $vers==4) && ($family==2 || $family==10) && $sf==$family) {\n", proto, proto)
		if h.FileWrapper != layout.DirectFile {
			fmt.Fprintf(&b, "    $file=*(uint64 *)($file+%d);\n", h.FileWrapper)
		}
		fmt.Fprintf(&b, "    $inode=*(uint64 *)($file+%d); $sb=*(uint64 *)($inode+%d);\n", o.FileInode, o.InodeSB)
		fmt.Fprintf(&b, "    if (@cp[tid,%d]) { @overwritten=count(); }\n", i)
		fmt.Fprintf(&b, "    @cp[tid,%d]=$countp; @write[tid,%d]=%d;\n", i, i, write)
		fmt.Fprintf(&b, "    @uid[tid,%d]=*(uint32 *)($rq+%d); @gid[tid,%d]=*(uint32 *)($rq+%d);\n", i, o.RqUID, i, o.RqGID)
		fmt.Fprintf(&b, "    @ino[tid,%d]=*(uint64 *)($inode+%d); @dev[tid,%d]=*(uint32 *)($sb+%d);\n", i, o.InodeIno, i, o.SBDev)
		fmt.Fprintf(&b, "    if ($family==2) {\n      @cip[tid,%d]=ntop(2,*(uint32 *)($rq+%d)); @sip[tid,%d]=ntop(2,*(uint32 *)($rq+%d));\n    } else {\n      @cip[tid,%d]=ntop(10,((struct nfsmon_ipv6 *)($rq+%d))->bytes); @sip[tid,%d]=ntop(10,((struct nfsmon_ipv6 *)($rq+%d))->bytes);\n    }\n  }\n}\n", i, o.RqAddr+4, i, o.RqDaddr+4, i, o.RqAddr+8, i, o.RqDaddr+8)
		fmt.Fprintf(&b, "kretprobe:%s\n/@cp[tid,%d]/\n{\n", h.Symbol, i)
		fmt.Fprintf(&b, "  if ((uint32)retval==0) {\n    $bytes=*(uint%d *)@cp[tid,%d];\n    $cip=@cip[tid,%d]; $sip=@sip[tid,%d];\n", h.CountBytes*8, i, i, i)
		key := "$cip,$sip"
		if group != "clients" && group != "servers" {
			fmt.Fprintf(&b, "    $uid=@uid[tid,%d]; $gid=@gid[tid,%d]; $dev=@dev[tid,%d]; $ino=@ino[tid,%d];\n", i, i, i, i)
			key += ",$uid,$gid,$dev,$ino"
		}
		fmt.Fprintf(&b, "    if (@write[tid,%d]) { @wb[%s]=sum($bytes); @wo[%s]=count(); }\n    else { @rb[%s]=sum($bytes); @ro[%s]=count(); }\n  } else { @failed=count(); }\n", i, key, key, key, key)
		for _, m := range []string{"cp", "write", "cip", "sip", "uid", "gid", "dev", "ino"} {
			fmt.Fprintf(&b, "  delete(@%s[tid,%d]);\n", m, i)
		}
		b.WriteString("}\n")
	}
	fmt.Fprintf(&b, "interval:ms:%d\n{\n  printf(\"NFSMON_BEGIN\\n\");\n  print(@rb); print(@wb); print(@ro); print(@wo);\n  print(@failed); print(@overwritten);\n  printf(\"NFSMON_END\\n\");\n}\n", interval.Milliseconds())
	b.WriteString("END { clear(@rb); clear(@wb); clear(@ro); clear(@wo); clear(@failed); clear(@overwritten); clear(@cp); clear(@write); clear(@cip); clear(@sip); clear(@uid); clear(@gid); clear(@dev); clear(@ino); }\n")
	return b.String()
}
