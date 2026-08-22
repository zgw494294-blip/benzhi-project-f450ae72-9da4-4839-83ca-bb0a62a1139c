# caption-delivery-qc

`caption-delivery-qc` 是面向公共广播节目字幕交付的质量审校服务。服务以审校任务为聚合边界，支持字幕段编制、冻结送审、问题退修与回应、复审批准、交付凭据签发以及时间线和凭据校验。

## 构建与测试

```bash
go build ./cmd/captionqc
go test ./...
```

## 运行

默认仅监听高位回环地址 `127.0.0.1:19081`：

```bash
go run ./cmd/captionqc
```

可以使用 `-addr=127.0.0.1:19082` 指定地址，也可以通过端口号形式的 `PORT` 配置绑定到 `127.0.0.1:<PORT>`。服务提供 `GET /healthz` 健康检查。

## 自检

以下命令启动临时 HTTP 服务，通过真实回环请求完成创建任务、字幕登记、首轮退修、修订回应、复审批准和签发凭据，随后自动退出：

```bash
go run ./cmd/captionqc -selfcheck -addr=127.0.0.1:19081
```

JSON API 位于 `/api/v1/review-jobs`。写请求使用 `X-Operator` 传递操作者，创建任务可使用 `Idempotency-Key`，聚合变更通过请求体中的 `expectedVersion` 执行乐观并发控制。

草稿可通过 `PATCH /api/v1/review-jobs/{jobID}` 修订节目元数据，请求必须同时提供 `X-Operator`、`Idempotency-Key` 和 `expectedVersion`。同一任务详情入口使用 `revision` 和可选的 `candidateDigest` 查询不可变冻结候选，响应中的字幕按 `sequence` 排序。

字幕可通过 `POST /api/v1/review-jobs/{jobID}/cues` 的 `operations` 数组批量新增、修改和删除；增加 `preflight=true` 或使用 `.../cues/preflight` 可执行只读导入试算，单批最多 100 项。退修编辑还必须提交当前冻结候选的 `baseRevision` 和 `baseDigest`。`GET .../{jobID}/submit/preflight` 提供送审前质量预检。

问题支持在 `findings` 路径批量登记、条件分页查询及批量回应，`findings/preflight` 返回退修闭环预检。结束审校前先查询 `GET .../findings/finish?reviewRound=<n>` 取得 `findingSummaryDigest`，提交时必须携带该摘要和非空 `conclusionNote`。

`approval/readiness` 返回候选摘要、审校结论摘要、检查项版本和确定性 `checklistDigest`；批准请求必须同时提交 `expectedVersion`、`candidateRevision`、`candidateDigest`、`checklistDigest` 和签字备注。`diff/{fromRevision}/{toRevision}` 返回两个冻结候选的字幕差异，任务内 `verify` 可校验单份凭据。`POST /api/v1/verify` 接受最多 50 份 `credentials`，按输入顺序返回内容、事件链和凭据摘要的分项结果及失败分类汇总。

字幕批量导入可通过 `Idempotency-Key` 或 `importIdempotencyKey` 幂等重放；送审预检会检查节目首尾覆盖和规则集间隙阈值。问题记录带有稳定 `fingerprint` 与历史关联，退修回应需提交对应字幕的 `cueContentHash`。`findings?summary=true` 返回按轮次的闭环趋势统计。`timeline` 支持 `cursor`、`limit`、`type`、`actor` 分页筛选并校验事件链完整性，元数据修订、审校结论、退修基线证明和批准清单均会保存在时间线事件中。
