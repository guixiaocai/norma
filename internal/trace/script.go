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
		fmt.Fprintf(&b, "kprobe:%s\n{\n  $started=nsecs; $rq=(uint64)%s; $file=(uint64)%s; $countp=(uint64)%s;\n", h.Symbol, arg(h.RqArg), arg(h.FileArg), arg(h.CountArg))
		o := l.Offsets
		fmt.Fprintf(&b, "  $proto=*(uint32 *)($rq+%d); $vers=*(uint32 *)($rq+%d); $family=*(uint16 *)($rq+%d); $sf=*(uint16 *)($rq+%d);\n", o.RqProt, o.RqVers, o.RqAddr, o.RqDaddr)
		fmt.Fprintf(&b, "  if ($rq && (%d==0 || $proto==%d) && ($vers==3 || $vers==4) && ($family==2 || $family==10) && $sf==$family) {\n", proto, proto)
		if h.FileWrapper != layout.DirectFile {
			fmt.Fprintf(&b, "    $file=*(uint64 *)($file+%d);\n", h.FileWrapper)
		}
		fmt.Fprintf(&b, "    $inode=*(uint64 *)($file+%d); $sb=*(uint64 *)($inode+%d);\n", o.FileInode, o.InodeSB)
		fmt.Fprintf(&b, "    if (@started[tid,%d]) { @overwritten=count(); }\n", i)
		fmt.Fprintf(&b, "    @started[tid,%d]=$started; @cp[tid,%d]=$countp; @write[tid,%d]=%d; @proto[tid,%d]=$proto; @detail[tid,%d]=($file && $inode && $sb);\n", i, i, i, write, i, i)
		fmt.Fprintf(&b, "    @uid[tid,%d]=*(uint32 *)($rq+%d); @gid[tid,%d]=*(uint32 *)($rq+%d);\n", i, o.RqUID, i, o.RqGID)
		fmt.Fprintf(&b, "    @ino[tid,%d]=*(uint64 *)($inode+%d); @dev[tid,%d]=*(uint32 *)($sb+%d);\n", i, o.InodeIno, i, o.SBDev)
		fmt.Fprintf(&b, "    if ($family==2) {\n      @cip[tid,%d]=ntop(2,*(uint32 *)($rq+%d)); @sip[tid,%d]=ntop(2,*(uint32 *)($rq+%d));\n    } else {\n      @cip[tid,%d]=ntop(10,((struct nfsmon_ipv6 *)($rq+%d))->bytes); @sip[tid,%d]=ntop(10,((struct nfsmon_ipv6 *)($rq+%d))->bytes);\n    }\n  }\n}\n", i, o.RqAddr+4, i, o.RqDaddr+4, i, o.RqAddr+8, i, o.RqDaddr+8)
		fmt.Fprintf(&b, "kretprobe:%s\n/@started[tid,%d]/\n{\n", h.Symbol, i)
		fmt.Fprintf(&b, "  $elapsed=nsecs-@started[tid,%d]; $cip=@cip[tid,%d]; $sip=@sip[tid,%d]; $proto=@proto[tid,%d]; $write=@write[tid,%d];\n", i, i, i, i, i)
		b.WriteString("  $us=$elapsed/1000+($elapsed%1000!=0); $v=$us>0 ? $us-1 : 0; $bucket=0;\n")
		for _, shift := range []uint{32, 16, 8, 4, 2, 1} {
			fmt.Fprintf(&b, "  if ($v>=%d) { $v=$v>>%d; $bucket=$bucket+%d; }\n", uint64(1)<<shift, shift, shift)
		}
		b.WriteString("  if ($v) { $bucket=$bucket+1; } if ($bucket>31) { $bucket=31; }\n")
		b.WriteString("  @dc[$cip,$sip,$proto,$write]=count(); @dn[$cip,$sip,$proto,$write]=sum($elapsed); @dm[$cip,$sip,$proto,$write]=max($elapsed); @dh[$cip,$sip,$proto,$write,$bucket]=count();\n")
		b.WriteString("  if ((uint32)retval!=0) { @de[$cip,$sip,$proto,$write]=count(); @failed=count(); }\n")
		fmt.Fprintf(&b, "  else if (@cp[tid,%d]) {\n    $bytes=*(uint%d *)@cp[tid,%d];\n", i, h.CountBytes*8, i)
		b.WriteString("    if ($write) { @cwb[$cip,$sip,$proto]=sum($bytes); @cwo[$cip,$sip,$proto]=count(); } else { @crb[$cip,$sip,$proto]=sum($bytes); @cro[$cip,$sip,$proto]=count(); }\n")
		fmt.Fprintf(&b, "    if (@detail[tid,%d]) {\n      $uid=@uid[tid,%d]; $gid=@gid[tid,%d]; $dev=@dev[tid,%d]; $ino=@ino[tid,%d];\n", i, i, i, i, i)
		key := "$cip,$sip,$proto,$uid,$gid,$dev,$ino"
		fmt.Fprintf(&b, "      if ($write) { @wb[%s]=sum($bytes); @wo[%s]=count(); }\n      else { @rb[%s]=sum($bytes); @ro[%s]=count(); }\n    } else { @badreads=count(); }\n  } else { @badreads=count(); }\n", key, key, key, key)
		for _, m := range []string{"started", "cp", "write", "proto", "detail", "cip", "sip", "uid", "gid", "dev", "ino"} {
			fmt.Fprintf(&b, "  delete(@%s[tid,%d]);\n", m, i)
		}
		b.WriteString("}\n")
	}
	maps := []string{"rb", "wb", "ro", "wo", "crb", "cwb", "cro", "cwo", "dc", "de", "dn", "dm", "dh", "failed", "overwritten", "badreads"}
	fmt.Fprintf(&b, "interval:ms:%d\n{\n  printf(\"NFSMON_BEGIN\\n\");\n", interval.Milliseconds())
	for _, name := range maps {
		fmt.Fprintf(&b, "  print(@%s);\n", name)
	}
	b.WriteString("  printf(\"NFSMON_END\\n\");\n}\nEND { ")
	for _, name := range append(maps, "started", "cp", "write", "proto", "detail", "cip", "sip", "uid", "gid", "dev", "ino") {
		fmt.Fprintf(&b, "clear(@%s); ", name)
	}
	b.WriteString("}\n")
	return b.String()
}
