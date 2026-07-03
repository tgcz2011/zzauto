package agents

import "errors"

// 本文件定义 Evaluator agent 用于向编排器表达「讨论/评估状态」的哨兵错误。
//
// Agent 接口的 Run 仅返回 error，编排器需要区分「正常完成」与「需继续
// 循环」两种状态，故用哨兵 error 表达，编排器用 errors.Is 判定：
//
//   - Designer↔Evaluator 讨论循环：
//   - Evaluator 返回 nil          → 达成共识，进入下一阶段
//   - Evaluator 返回 ErrNoConsensus → 未达成共识，继续下一轮讨论
//
//   - Evaluator→Generator 评估循环：
//   - Evaluator 返回 nil                → 评估通过，进入 Gittor
//   - Evaluator 返回 ErrEvaluationFailed → 评估不通过，回到 Generator 重试
//
// 其他 agent 正常返回 nil（成功）或任意非哨兵 error（失败，终止流程）。
// 编排器在判定哨兵前应先排除其他错误：非哨兵 error 一律视为失败。

// ErrNoConsensus 表示 Designer↔Evaluator 讨论尚未达成共识，需继续下一轮。
var ErrNoConsensus = errors.New("consensus not reached")

// ErrEvaluationFailed 表示 Evaluator 评估 Generator 产出不合格，需重试。
var ErrEvaluationFailed = errors.New("evaluation failed")
