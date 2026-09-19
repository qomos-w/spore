# spore

spore 是一个 Go 的脚本优先嵌入层：用一门轻量静态类型语言写行为，把 Go 函数与 struct 注册为宿主能力，再从 Go 以结构化类型安全的方式调用脚本 callable。

[English](README.md)

> 状态：早期开发阶段。`script` 包的嵌入面是稳定契约；`internal/` 包可随时变更。

```go
rt, _ := script.NewRuntime()
rt.BindFunc("math", "add", func(a, b int) int { return a + b })
rt.LoadSource("demo", `export fun main(): int { return add(1, 2) }`)
result, _ := rt.Call("main")
var n int
result.DecodeInto(&n) // 3
```

## 安装

```bash
go get github.com/qomos-w/spore
```

要求 Go 1.27+。

## 快速上手

```go
package main

import (
    "fmt"
    "log"

    "github.com/qomos-w/spore/script"
)

func main() {
    rt, err := script.NewRuntime()
    if err != nil {
        log.Fatal(err)
    }

    // 1. 在命名空间下注册宿主函数
    rt.BindFunc("math", "add", func(a, b int) int { return a + b })
    rt.BindValue("config", "version", "1.0.0")

    // 2. 加载脚本源码
    err = rt.LoadSource("demo", `
        import { add, version } from "math"

        export fun greet(name: string): string {
            return "hello " + name + " (v" + version + ")"
        }
    `)
    if err != nil {
        log.Fatal(err)
    }

    // 3. 调用
    result, err := rt.Call("greet", "world")
    if err != nil {
        log.Fatal(err)
    }
    if err := result.Unwrap(); err != nil {
        log.Fatal(err)
    }

    var s string
    if err := result.DecodeInto(&s); err != nil {
        log.Fatal(err)
    }
    fmt.Println(s) // hello world (v1.0.0)
}
```

## 脚本语言

sporescript 是一门静态类型语言，编译到基于栈的字节码。主要特性：

- **类型**：`int`、`long`、`float`、`double`、`bool`、`string`、`bytes`、数组、有序 map、struct、class、interface
- **函数**：`fun`、`export fun`、`stream fun`（生成器式流）
- **控制流**：`if`/`else`、`when`/`case`（值/类型匹配 + guard）、`for in`、`while`、`try`/`catch`
- **闭包**：lambda 表达式，引用捕获，函数类型编译期检查
- **模块**：`import`/`from`（别名与命名列表）、`export`（含编译期 `export struct`/`export type`）

完整语言参考见 [SYNTAX.zh-CN.md](SYNTAX.zh-CN.md)。

## 类型映射

| 脚本类型 | Go 类型 | 说明 |
|---|---|---|
| `int` | `int32` | 32 位有符号 |
| `long` | `int64` | 64 位有符号 |
| `uint` | `uint32` | 32 位无符号 |
| `ulong` | `uint64` | 64 位无符号 |
| `float` | `float32` | 32 位 IEEE-754 |
| `double` | `float64` | 64 位 IEEE-754 |
| `bool` | `bool` | |
| `string` | `string` | |
| `bytes` | `[]byte` | |
| `T[]` | `[]T` | slice |
| `map<K,V>` | `map[K]V` | 有序 map（K 仅限 string） |
| `struct` | Go struct | 值语义深拷贝 |

**边界处做收窄检查**：把超出 `int32` 范围的 Go `int64` 传给脚本 `int` 参数会产生运行时错误，而不是静默截断。

## Schema 与代码生成

Schema 是跨语言契约的唯一事实来源，`cmd/` 生成器把 schema 清单变成两侧的类型化代码：

| 工具 | 产物 |
|---|---|
| `spore-gen-go-types` | 从 schema 清单生成 Go 类型定义 |
| `spore-gen-go-server` | 从 schema 清单生成 Go server 桩 |
| `spore-gen-ts` | 从 schema 清单生成 TypeScript 类型 |
| `spore-gen-ts-client` | 从 schema 清单生成 TypeScript client 类 |

## 目录结构

| 路径 | 内容 |
|---|---|
| `script/` | 公开宿主嵌入 API（`Runtime`、`Result`、`Bind*`） |
| `schema/` | 权威 schema 描述符（`TypeDesc`、`ObjectDesc`、`CallableDesc`） |
| `binding/` | 宿主绑定层（能力、调用、数据投影） |
| `transport/` | schema 感知的 JSON codec 与线格式 envelope |
| `std/` | 标准库模块（`math`、`strings`、`json`、`time` 等） |
| `ts/` | TypeScript 运行时镜像 |
| `benchmarks/` | 与 goja / lua / tengo 的对比基准 |
| `internal/script/` | 编译器、VM、前端（internal，宿主不导入） |

## 文档

| 文档 | 用途 |
|---|---|
| [SYNTAX.zh-CN.md](SYNTAX.zh-CN.md) | 脚本语言参考：类型、语法、语义 |

## 稳定性

公开面（`script.Runtime` 及其方法）是稳定嵌入契约；`internal/` 包不承诺兼容。TypeScript 镜像跟随 Go 侧 schema 演进。

## 许可证

[MIT](LICENSE)
