package agents

import "errors"

// 本文件定义 agent 用于向编排器表达「状态」的哨兵错误。
//
// Agent 接口的 Run 仅返回 error，编排器需要区分「正常完成」与「需重试/
// 重跑」两种状态，故用哨兵 error 表达，编排器用 errors.Is 判定：
//
//   - Reviewer→Coder 评估循环：
//   - Reviewer 返回 nil                → 评估通过，进入下一阶段
//   - Reviewer 返回 ErrEvaluationFailed → 评估不通过，回到 Coder 重试
//
//   - Mixor 处理新需求：
//   - Mixor 返回 nil          → 合并成功，从 Planner 继续
//   - Mixor 返回 ErrNeedRerun → 与现有产出冲突，从 Analyst 重跑
//
//   - 编排器被用户停止：
//   - 编排器在 ctx.Done 或收到停止信号时返回 ErrStopped
//
// 其他 agent 正常返回 nil（成功）或任意非哨兵 error（失败，终止流程）。
// 编排器在判定哨兵前应先排除其他错误：非哨兵 error 一律视为失败。

// ErrEvaluationFailed 表示 Reviewer 评估 Coder 产出不合格，需重试。
var ErrEvaluationFailed = errors.New("evaluation failed")

// ErrNeedRerun 表示 Mixor 判定新需求与现有产出冲突，需要从 Analyst 重跑。
var ErrNeedRerun = errors.New("need rerun from analyst")

// ErrStopped 表示编排器被用户停止。
var ErrStopped = errors.New("orchestrator stopped by user")
