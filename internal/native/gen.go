// SPDX-License-Identifier: MIT
package native

//go:generate go tool bpf2go -cc clang -target bpfel -tags linux -type key_t -type val_t -type config_t -type health_t -type name_t collector bpf/collector.c -- -D__TARGET_ARCH_x86 -I/usr/include
