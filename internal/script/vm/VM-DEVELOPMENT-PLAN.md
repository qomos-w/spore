# VM Phase 2 开发计划

> 最后更新: 2026-08-27
> 状态: **已完成** — NaN-boxing、非移动 GC、直接指针 三大 Step 均已落地，详见 `TDD.md` §9.8 checklist 与 §9.9 roadmap。本文档保留为规划过程的历史记录;具体实现以代码与 TDD.md 为准。

---

## 0. 当前基线评估

### 0.1 代码规模

| 文件 | 行数 | 职责 |
|------|------|------|
| value.go | 164 | 3-bit tag 编解码、类型谓词 |
| vm.go | 395 | VM 核心：内存分配、handle、numeric、栈、字符串 |
| gc.go | 283 | Mark-Sweep + Compaction、numericArea sweep |
| object.go | 83 | class 实例分配与字段访问 |
| struct.go | 199 | struct 定义、注册、实例化 |
| array.go | 178 | 数组（内联 + 分段）、动态增长 |
| map.go | 278 | 有序 map（开放寻址 + 链表） |
| string.go | 168 | 三级字符串池（small/medium/large） |
| class.go | 293 | class 定义、vtable、接口、继承 |
| function.go | 82 | 函数注册与调用 |
| types.go | 175 | typeID 编码（basic/array/map/class/struct） |
| export.go | ? | 导出层 |
| stacktrace.go | ? | 调用栈 |
| **合计** | **~2,333** | |

### 0.2 测试覆盖

| 文件 | 测试数 | 覆盖范围 |
|------|--------|---------|
| value_test.go | 14 | tag 隔离、bool/int/uint/float/handle/string round-trip |
| gc_test.go | 22 | mark、sweep、compaction、numericArea、handle 更新、自动触发 |
| vm_test.go | 7 | 内存分配、对象、字符串、numeric、struct、栈 |
| array_test.go | 2 | 小数组、push 扩容 |
| map_test.go | 4 | CRUD、保序、覆写 |
| **合计** | **49** | |

### 0.3 当前架构瓶颈

| 瓶颈 | 根因 | 影响 |
|------|------|------|
| long/ulong/double 间接访问 | numericArea 索引 → 额外查表 + 独立 GC 扫描 | 数值运算热路径慢 |
| 指针两次查表 | value → handleTable → memoryPool | 每次字段/元素访问多一次间接 |
| GC 移动式 compaction | sweep 阶段移动存活对象 + 更新 handleTable | GC 暂停与存活对象成正比 |
| float64 非零开销 | 存入 numericArea，编解码需要 float64bits 转换 | 浮点运算频繁场景受限 |
| 栈式字节码 | 操作数通过栈传递，每条指令隐含 push/pop | 指令数量多，寄存器利用率低 |

---

## 1. NaN-boxing 值编码（Step 1）

> 优先级: **最高** — 消除 numericArea，统一值表示
> 预计改动: value.go（重写）、vm.go（删除 numeric 相关）、gc.go（删除 numericArea sweep）
> 依赖: 无，可独立实施
> 风险: 中等 — 所有值操作都受影响，需要全量测试覆盖

### 1.1 目标编码

```
64-bit NaN-boxed value:
┌─────────┬──────────┬──────────────────────────────────┐
│ 8-bit   │ 4-bit    │ 52-bit payload                   │
│ 0xFF    │ tag      │ (type-specific)                  │
└─────────┴──────────┴──────────────────────────────────┘

float64 例外: 整个 64-bit = 原始 IEEE 754 double
判断规则: exponent ≠ 0x7FF → 正常 double，零开销
```

### 1.2 Tag 分配

```
Tag  常量名           类型             Payload
──────────────────────────────────────────────────────────
0    tagNull          null             无（全零）
1    tagBool          bool             [0] = 0/1
2    tagInt32         int32            signed 32-bit（符号扩展至 52 位）
3    tagFloat64       float64          整个 64-bit = 原始 double
4    tagLongInline    long             ≤2^53 的整数（53 位含符号）
5    tagPointer       pointer          48-bit memoryPool 索引
6    tagStrInline     string           ≤6 字节短字符串（4-bit 长度 + 数据）
7    tagStrPtr        string           memoryPool 字符串对象索引
8    tagLongHeap      long             memoryPool 中 long 堆对象索引
9    tagULongInline   ulong            ≤2^53 无符号整数
10   tagULongHeap     ulong            memoryPool 中 ulong 堆对象索引
11   tagDoubleHeap    double           memoryPool 中 double 堆对象（保留）
12-15 (reserved)      BigInt/Decimal   未来扩展
```

### 1.3 实施任务清单

#### T1.1 — 定义 NaN-boxing 常量与原语（value.go）

- [x] 定义 NaN 前缀常量 `nanPrefix = 0xFFFC000000000000`
- [x] 定义 tag 常量（0-15）
- [x] 实现 `makeNaN(tag uint64, payload uint64) value`
- [x] 实现 `v.tag() uint64` — 提取 4-bit tag
- [x] 实现 `v.payload() uint64` — 提取 52-bit payload
- [x] 实现 `v.isFloat64() bool` — 判断 exponent ≠ 0x7FF
- [x] 实现 `v.isNull() bool` — tag == tagNull
- [x] 编写 `TestNaNBoxingPrimitive` 验证编解码正确性

#### T1.2 — 实现各类型的 encode/decode（value.go）

- [x] `encodeNull() value` / `decodeNull()`
- [x] `encodeBool(bool) value` / `decodeBool() bool`
- [x] `encodeInt32(int32) value` / `decodeInt32() int32`
- [x] `encodeFloat64(float64) value` — 直接返回 value(math.Float64bits(f))
- [x] `decodeFloat64() float64` — 直接 math.Float64frombits(uint64(v))
- [x] `encodeLongInline(int64) value` — ≤2^53 内联
- [x] `decodeLongInline() int64`
- [x] `encodeULongInline(uint64) value` — ≤2^53 内联
- [x] `decodeULongInline() uint64`
- [x] `encodePointer(idx uint64) value` — 48-bit pool 索引
- [x] `decodePointer() uint64`
- [x] `encodeStrInline([]byte) value` — ≤6 字节
- [x] `decodeStrInline() []byte`
- [x] 每个函数配套 round-trip 测试

#### T1.3 — Long/ULong 堆对象（vm.go + 新增 value_helpers.go）

- [x] 在 memoryPool 中定义 long 堆对象 header 格式:
  `[GC_mark:1 | type_marker:3 | 0:28 | size:32] + [payload: 8 bytes]`
- [x] 实现 `encodeLong(int64) value` — 内联优先，超出走堆对象
- [x] 实现 `decodeLong(value) int64`
- [x] 实现 `encodeULong(uint64) value`
- [x] 实现 `decodeULong(value) uint64`
- [x] 实现 `encodeDouble(float64) value` — 直接 NaN-boxing（零开销）
- [x] 实现 `decodeDouble(value) float64`
- [x] 编写 `TestLongInlineVsHeap` — 边界值测试 (2^53-1, 2^53, -2^53)
- [x] 编写 `TestULongInlineVsHeap` — 边界值测试
- [x] 编写 `TestDoubleZeroOverhead` — 验证 float64 不走 NaN 路径

#### T1.4 — 删除 numericArea（vm.go + gc.go）

- [x] 删除 vm 结构体字段: `numericArea`, `numericTop`, `numericFreeList`
- [x] 删除 vm 方法: `allocNumericSlot()`
- [x] 删除 gc 方法: `sweepNumericArea()`
- [x] 从 `newVM()` 中移除 numericArea 初始化
- [x] 从 `gc.collect()` 中移除 `sweepNumericArea()` 调用
- [x] 运行全量测试确认无回归

#### T1.5 — 更新类型谓词（value.go）

- [x] `isInt()`, `isFloat()`, `isBool()`, `isString()`, `isPointer()`
- [x] `isLong()`, `isULong()`, `isDouble()`
- [x] `isNumeric()` — 包含 int32 + long + ulong + float64
- [x] `isNull()` — 更新为检查 tagNull

#### T1.6 — 更新下游消费者

- [x] string.go — 更新 `encodeString`/`decodeString` 中的 tag 判断
- [x] object.go — 更新字段读写中的 pointer 编解码
- [x] struct.go — 同上
- [x] array.go — 更新元素访问中的 value 编解码
- [x] map.go — 更新 key/value 操作中的 hash 和比较
- [x] class.go — 确认 vtable 查找不受影响
- [x] function.go — 确认参数传递不受影响

### 1.4 测试策略

| 测试类 | 内容 |
|--------|------|
| 单元测试 | 每个类型的 encode/decode round-trip |
| 边界测试 | long 2^53 边界、float64 NaN/Inf/零、空字符串、满 payload |
| 标签隔离 | 不同 tag 的值不混淆 |
| GC 集成 | 标记堆 long/ulong 对象，sweep 回收 |
| 全量回归 | 现有 49 个测试全部通过 |

### 1.5 验收标准

- [x] numericArea 代码完全删除，无残留引用
- [x] float64 编解码零开销（直接位模式）
- [x] long ≤2^53 内联，超出走堆对象
- [x] 所有现有测试通过 + 新增 NaN-boxing 专项测试 ≥ 20 个

---

## 2. 非移动 GC（Step 2）

> 优先级: **高** — 消除 compaction 暂停，为直接指针奠基
> 预计改动: gc.go（重写 sweep）、vm.go（freeList）
> 依赖: 无，可与 Step 1 并行开发
> 风险: 中等 — GC 是内存安全的基础

### 2.1 当前 GC 流程

```
collect():
  mark()             ← 从栈扫描，递归标记可达对象（不变）
  sweep()            ← 移动式压缩：存活对象前移，更新 handleTable
  sweepNumericArea() ← 回收不可达 numeric slot（Step 1 后已删除）
```

### 2.2 目标 GC 流程

```
collect():
  mark()    ← 从栈扫描，递归标记可达对象（与当前相同）
  sweep()   ← 非移动式：释放未标记 slot，加入 freeList
```

### 2.3 实施任务清单

#### T2.1 — 添加 freeList 数据结构（vm.go）

- [x] 新增字段: `freeList []freeSlot` 或 `freeRanges`
- [x] `freeSlot` 结构: `{start int, size int}`
- [x] 实现 `allocFromFreeList(size int) (int, bool)` — 尝试从 freeList 分配
- [x] 修改 `allocMemory()` — 优先查 freeList，不足再从 memTop 分配
- [x] 实现 `freeRange(start, size int)` — 将释放的空间加入 freeList
- [x] 编写 `TestFreeListAlloc` / `TestFreeListReuse`

#### T2.2 — 重写 sweep 为非移动式（gc.go）

- [x] 新的 `sweep()`:
  1. 遍历 memoryPool [1, memTop)
  2. 已标记对象: 清除 mark bit
  3. 未标记对象: 调用 `freeRange(idx, objSize)`，加入 freeList
  4. **不移动任何存活对象**
- [x] 删除 `oldToNew` 映射逻辑
- [x] 删除 handleTable 更新逻辑（compaction 专属）
- [x] 删除对象前移拷贝代码
- [x] 编写 `TestSweepNonMoving` — 验证存活对象地址不变
- [x] 编写 `TestSweepFreeListPopulation` — 验证释放空间进入 freeList

#### T2.3 — 更新 mark 阶段

- [x] `mark()` 基本不变 — 仍从栈扫描
- [x] `markValue()` 需适配新 tag（如果 Step 1 已合并）
- [x] `markFromIndex()` — 确认能正确处理 long-heap 对象
- [x] 确保 mark 递归深度保护（当前 10,000 级）不变

#### T2.4 — GC 触发策略优化

- [x] 当前: `memTop > memorySize * 3/4`（单一阈值）
- [x] 目标: 增加分配失败时触发（freeList 无法满足时）
- [x] 可选: 添加 GC 统计（回收率、暂停次数）用于调优
- [x] 编写 `TestGCTriggerOnAllocation` — 已有，确认仍通过

#### T2.5 — 删除 compaction 相关代码

- [x] 删除 sweep 中的对象前移拷贝
- [x] 删除 `oldToNew map[int]int`
- [x] 删除 handleTable 批量更新循环
- [x] 删除 `TestGC_CompactionReducesMemTop` — 不再适用
- [x] 删除 `TestGC_HandleTableInvalidation` — handle 不再因 GC 失效

### 2.4 测试策略

| 测试类 | 内容 |
|--------|------|
| 非移动验证 | GC 前后存活对象的 memoryPool 索引不变 |
| freeList 正确性 | 释放空间可被后续分配重用 |
| 碎片容忍 | 多次分配/释放后 freeList 仍能正确合并或使用 |
| 全量回归 | 现有 GC 测试中非 compaction 相关的仍通过 |

### 2.5 验收标准

- [x] sweep 阶段不移动任何存活对象
- [x] handleTable 在 GC 后不需要更新
- [x] freeList 正确管理释放空间
- [x] 所有现有测试通过（除 compaction 相关测试被替换）

---

## 3. 直接指针（Step 3）

> 优先级: **中** — 性能收益最大的步骤
> 预计改动: value.go（pointer tag 编码）、vm.go（删除 handleTable）、gc.go（mark 路径）
> 依赖: Step 1 + Step 2（需要 NaN-boxing 的 48-bit payload + 非移动 GC）
> 风险: **高** — 每个对象访问点都受影响

### 3.1 当前指针路径

```
value(tag=pointer, payload=handle)
  → handleTable[handle] → memoryPool[index]     // 两次查表
```

### 3.2 目标指针路径

```
value(tag=pointer, payload=48-bit pool_index)
  → memoryPool[pool_index]                       // 一次查表
```

### 3.3 实施任务清单

#### T3.1 — 删除 handle 层（vm.go）

- [x] 删除 vm 字段: `handleTable []int`, `nextHandle int`
- [x] 删除 vm 方法: `createHandle()`, `getMemoryIndex()`, `resolveHandle()`
- [x] 修改 `allocMemory()` — 返回值直接作为 pointer payload
- [x] 所有 `allocMemory()` 调用点：不再调用 `createHandle()`
- [x] 编写 `TestDirectPointerRoundTrip`

#### T3.2 — 更新 pointer 编解码（value.go）

- [x] `encodePointer(idx int) value` — `NaN_prefix | (tagPointer << 52) | uint64(idx)`
- [x] `decodePointer() int` — `int(v.payload())`
- [x] 确保 48-bit 索引范围检查
- [x] `isPointer()` / `isNull()` 更新

#### T3.3 — 更新所有对象创建点

- [x] object.go: `newObject()` 返回 pool index 而非 handle
- [x] struct.go: `newStruct()` 同上
- [x] array.go: `newArray()` / `newSmallArray()` / `newSegmentedArray()` 同上
- [x] map.go: `newMap()` 同上
- [x] string.go: 大字符串分配同上
- [x] 更新所有 `getObjectField` / `setObjectField` 等方法签名

#### T3.4 — 更新 GC mark 阶段

- [x] `markValue()` — 从 value 直接提取 pool index，不再经过 handleTable
- [x] `markFromIndex()` — 无变化（本来就用 index）
- [x] 删除 mark 阶段中的 handle 解析代码

#### T3.5 — 更新测试

- [x] 删除所有 `handle` 相关测试
- [x] 新增 `TestDirectPointerAccess` — 验证对象直接通过 pool index 访问
- [x] 新增 `TestDirectPointerGC` — GC 后指针仍有效（非移动 GC 保证）

### 3.4 验收标准

- [x] handleTable 代码完全删除
- [x] 每个对象访问从两次查表降为一次
- [x] pointer payload 为 48-bit pool index
- [x] 所有测试通过

---

## 4. 寄存器式执行（Step 4）

> 优先级: **中低** — 性能优化，独立于内存模型
> 预计改动: bytecode/instruction.go（新操作码）、bytecode/compiler.go（寄存器分配）、bytecode/interpreter.go（执行引擎）
> 依赖: 无，可与 Step 1-3 完全并行
> 风险: **高** — 编译器 + 解释器全面重写
> 当前决策: **暂停 / 回退到栈式执行**

### 4.1 原因

当前工作区已经验证：
- Step 1（NaN-boxing）完成
- Step 2（非移动 GC）完成
- Step 3（direct pointer / 删除 handleTable）完成
- `go test ./internal/script/...` 曾在栈式执行下通过

而 Step 4 的寄存器式改造在实现过程中暴露出较高复杂度：
- `instruction` / `compiler` / `interpreter` / `stream continuation` 需要同步迁移
- C-style `for`、`for-in`、assignment statement、yield/resume 等路径容易出现 stack/register mixed-mode 污染
- 当前最复杂的问题集中在 loop 与 stream 的行为一致性，而不是 VM memory model 本身

因此，当前阶段应优先保留稳定的 **stack-based bytecode/interpreter**，继续复用已经完成的新 VM 内存模型（NaN-boxing + non-moving GC + direct pointers），而不是继续扩大寄存器执行面的不稳定范围。

### 4.2 当前结论

- 早期脚本项目可提供的是 **stack-based** loop / assignment 语义参考，不提供可直接复用的 register VM
- 现阶段最稳的组合是：
  - **保留栈式 bytecode/interpreter**
  - **继续使用新的 VM 内存分配/值编码模型**
- 寄存器式执行保留为未来优化项，但不再作为当前实现主线

### 4.3 回退策略

- [ ] 回退 `internal/script/bytecode/instruction.go` 中的 register-only 扩展，恢复以 stack opcode/operand 为主的表示
- [ ] 回退 `internal/script/bytecode/compiler.go` 中的 `compileExpressionReg` / `emitReg` / temp register 分配路径
- [ ] 回退 `internal/script/bytecode/interpreter.go` 中的 register frame / register dispatch / stream register snapshot 逻辑
- [ ] 删除为寄存器路径新增的专用 helper / 测试或改回 stack-only 版本
- [ ] 重新确认 `go test ./internal/script/...` 全绿

### 4.4 后续保留事项

未来如果再次推进寄存器式执行，建议前提是：
- 先独立设计完整 register bytecode ABI
- 避免 stack/register mixed-mode 共存于同一控制流路径
- 先从一个极小子集（literal/local/arithmetic/return）做完整闭环，再逐步推广到 calls / loops / streams

### 4.5 验收标准（当前阶段）

- [ ] 寄存器改造相关未提交工作区改动已清理
- [ ] 栈式执行继续通过全部现有脚本测试
- [ ] 新 VM 内存模型与旧栈式执行稳定共存

---

## 4. 类型系统与 AI 友好性收敛（后续工作）

> 当前状态: **待规划 / 建议后续推进**

### 4.1 当前判断

当前语言类型系统整体上是 **显式注解优先 + 强类型倾向** 的：
- 类型信息主要来自显式注解（变量、返回值、容器元素、alias）
- 编译期与运行时都会执行类型边界检查
- 结构化诊断已经具备较好的机器可读性

这对实现和运行时是合理的，但如果目标是“AI 友好的脚本语言”，还需要进一步把规则收敛得更少、更稳、更容易自动修复。

### 4.2 建议主线

后续类型系统工作建议优先集中在：

1. **文档收敛**
   - 写清楚基础类型、复合类型、结构类型、alias、null 语义
   - 明确 `struct / class / interface` 边界
   - 明确数值默认规则与隐式兼容规则

2. **规则统一**
   - 统一 assignment / return / argument compatibility
   - 固定数值提升与比较规则
   - 固定 alias 是语义别名还是 nominal type
   - 固定 null 允许/禁止边界

3. **错误系统强化**
   - 所有类型错误都给稳定 diagnostic code/path
   - 错误中包含 expected/actual 信息
   - 让 AI 可以根据错误做单跳修复

4. **复杂完整链路测试**
   - 补充 struct/class/interface/array/map/alias/stream 的复杂 full-chain 测试
   - 验证“栈式 bytecode/interpreter + 新 VM 内存模型”在真实脚本结构下的稳定性

### 4.3 最小实施清单

- [ ] 新增/更新类型系统总览文档
- [ ] 明确数值默认规则（int/long/ulong/float/double）
- [ ] 明确 assignment / return / argument 统一兼容规则
- [ ] 明确 `struct / class / interface` 使用边界
- [ ] 明确 alias 语义
- [ ] 明确 null 语义
- [ ] 补 assignment / return / argument 类型矩阵测试
- [ ] 补复杂脚本完整链路测试
- [ ] 收紧类型错误的 diagnostic code/path/expected/actual

### 4.4 当前建议

在继续做新的执行模型优化之前，优先完成：
- 类型规则文档收敛
- 复杂链路测试
- 更大范围回归验证

这样能让当前“栈式执行 + 新 VM 内存模型”的稳定组合在面向 AI 的使用场景下更可靠。

---

## 5. 实施顺序与并行策略

```
Week 1-2:   ┌─ Step 1: NaN-boxing (value.go 重写) ──────────┐
            └─ Step 2: 非移动 GC (gc.go 重写) ──────────────┘
                                                          (并行)
Week 3:     Step 1 + Step 2 合并 + 集成测试
             ↓
Week 4-5:   Step 3: 直接指针 (依赖 1+2 合并完成)
```

### 并行约束

```
Step 1 ──┐
         ├──→ Step 3（必须等 1+2 都完成）
Step 2 ──┘
```

---

## 6. 风险与缓解

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|---------|
| NaN-boxing 边界 case（NaN 传入 double slot） | 中 | 高 | exhaustive bit-pattern 测试 |
| 非移动 GC 内存碎片化 | 低 | 中 | freeList 合并策略；预留 compaction 接口 |
| 直接指针迁移遗漏点 | 中 | 高 | grep 全量搜索 handleTable/createHandle 引用 |
| Step 1+2 合并冲突 | 中 | 中 | 两个 step 在不同文件（value.go vs gc.go），冲突面小 |

---

## 7. 预期收益

| 指标 | Phase 1（当前） | Phase 2（目标） | 提升 |
|------|----------------|----------------|------|
| long/ulong 访问 | 2次间接（numericArea） | 内联 0 间接 / 堆 1 间接 | ~2x |
| double 访问 | 2次间接 + bits 转换 | 0 间接（直接位模式） | ~3x |
| 指针访问 | 2次查表（handleTable + pool） | 1次查表（直接 pool） | ~2x |
| GC 暂停 | O(存活对象) 移动 + handle 更新 | O(死亡对象) 标记清除 | 显著降低 |
| 内存碎片 | 无（compaction） | 有（freeList 管理） | 可接受 |

---

## 8. Checklist: 开始前必须确认

- [ ] 现有 49 个测试全部绿色
- [ ] `go vet ./internal/script/vm/...` 无警告
- [ ] 理解 value.go 中每个 encode/decode 的调用链（下游消费者）
- [ ] 理解 gc.go 中 mark 递归的所有对象类型分支
- [ ] 确认 Step 1 和 Step 2 的文件改动不重叠（value.go vs gc.go）
