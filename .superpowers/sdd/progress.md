# SDD 进度 — 2026-07-27 ss机场订阅代理 计划(docs/superpowers/plans/2026-07-27-ss-obfs-proxy.md)
分支: develop/xuyang/ss-obfs-proxy  基点: 3edade6fa

## 本计划任务(12个)
- Task 1: complete — **判定结果:obfs 必需,Task 5 不能跳过**
  证据①纯 ss 连真实节点 → EOF(TCP 通但 ss 握手被丢弃)
  证据②裸 TLS ClientHello 探针 → 服务端 0 字节即断开。这**不**证伪 obfs:
  simple-obfs 服务端不主动回 ServerHello,要等内层 ss 有响应数据才连带发出;
  空壳 ClientHello 无合法 ss 载荷 → 内层报错关连接 → 0 字节。与"是 simple-obfs"自洽。
  ⇒ 握手字节数无法靠黑盒探针拿到,必须 obfs+ss 一起实现后对真实节点迭代(Task 5 Step 3/5 的做法)
- Task 2: complete (commits 8b516a5cd..d4ca0a5ae, review 通过: Spec ✅ / Approved)
- Task 3: complete (commits d4ca0a5ae..1c605d74a, review 通过: Spec ✅ / Approved)
- Task 8: complete (commits 1c605d74a..5d7b2ca11, review 通过: Spec ✅ / Approved)
- Task 9: 实现已提交 5d7b2ca11..56c7140ee,**审查 Needs fixes,修复中**
  Important: 响应 `skipped[]` 字段输出成大写 `Name`/`Reason`(clashsub.Skipped 无 json tag),
  契约要求小写;测试因编解码用同一个无 tag 类型而未能发现(盲区)。修复含"让测试能抓住该 bug"。
  实现者自审已修的两处经审查核实为真:①订阅URL密钥脱敏 sanitizeFetchErr ②UpdateProxy 的 Extra 不再无条件覆盖
- Task 9: 修复 20873c9e2 复审 **Approved**(新测试用 map[string]any + 原始JSON双重断言,tag删掉必失败)
- Task 4: complete (commits 56c7140ee..603b9e68c, review 通过: Spec ✅ / Approved)
- Task 5: **端到端打通**, commits 20873c9e2..c04158647, review **Approved**(11 Minor)
  审查质量极高:解码 fixture 七层长度自洽+从 random 解出时间戳证明抓包为真;
  复制到隔离沙箱用 1字节 dripConn 实测分段读;发现 CCS 后 32B 记录 = ss salt 长度,
  与 go-shadowsocks2 initWriter 单独 Write salt 严丝合缝 → 端到端声明可信
  **已派批量修复**(5项:host长度界/空ObfsHost/注释扩展顺序/分段读回归测试/alert断言)
- Task 6: complete (commits c04158647..f2918bcc8, review 通过: Spec ✅ / Approved,无 Issue)
- Task 7: 实现已提交 f2918bcc8..22ae0ae59,待审查
  **重要发现**:真实协议分派点在 `repository/http_upstream.go` 的
  `buildUpstreamTransportWithTLSFingerprint`,**不在 tlsfingerprint 包内**(计划假设有误);
  改动前 ss 会落入"未知协议回退"分支——隧道能通但**不带 TLS 指纹**
- Task 10/11/12: 未开始

## 🎉 全链路集成验证已通过(控制方实测,2026-07-27)
走真实生产调用链,非单元测试:
  service.Proxy.URL() → proxyurl.Parse(scheme=ss,未被升级socks5h)
  → proxyutil.ConfigureTransportProxy(设置DialContext,Transport.Proxy=nil)
  → 真实 HTTPS 请求 api.anthropic.com → **HTTP 401 + Anthropic标准错误体(带request_id)**
⇒ 从代理记录到上游 API 整条链路可用。方案可行性已彻底证实。

## Task 5 关键技术发现(极易踩,务必保留注释)
握手响应 = ServerHello 96B + ChangeCipherSpec 6B = 102B。
**但不能写成固定常量跳过**:紧跟 CCS 之后的记录仍标 0x16(伪装Finished),
载荷却已是真实数据(32B = ss salt)。按"跳到0x17为止"会吞掉salt,
导致 chacha20poly1305: message authentication failed。
最终实现:丢弃记录直到第一条CCS消费完,之后一律按5字节头解包。

## 待补(两个独立来源都指出,收口时处理)
- `dialer.go` 的 `NewDialer` 未校验 `ObfsMode=tls` 时 `ObfsHost` 非空
  (Task4审查预判"Task5换真实SNI后会产生难定位的连接失败" + Task5实现者自陈,两边独立撞上)

## Minor findings(留给终审统一处理)
- Task 2: `ent/schema/proxy.go` 的 extra 字段未声明 `.SchemaType(map[string]string{dialect.Postgres:"jsonb"})`,
  与同仓库 `account.credentials`/`account.extra` 写法不一致;ent 元数据是通用 TypeJSON 而手写迁移建的是 JSONB。
  本项目未启用 ent 自动迁移(无 client.Schema.Create 调用),故无运行时影响,纯一致性问题。
- Task 3: `TestExtraClearOnUpdate` 在 RED 阶段天然通过(改动前 Extra 本就 nil),TDD 证据链对该用例不够干净;
  但 GREEN 状态下该断言有真实判别力(漏掉 ClearExtra 分支会失败),对回归有效。
- Task 8: `parse.go:82-89` 跳过原因把 server/port、cipher/password 各自合并成一条,未细分具体缺哪个字段
  (plan-mandated,brief 示例代码即如此);`parse.go:68` yaml 解析错误前缀是英文,与其余中文 Reason 风格不一致。

## 环境事实(实测)
- 本机 **docker 可用**(27.5.1) → `internal/repository` 集成测试(testcontainers+postgres)能真跑
  命令:`go test -tags integration ./internal/repository/ -run TestProxyRepoSuite -count=1`
  首次会拉 postgres:18.1-alpine3.23 镜像,较慢
- 集成测试真实模式:testify suite + `testEntTx(s.T())` + `newProxyRepositoryWithSQL(tx.Client(), tx)`
  **计划 Task 3 brief 里写的 `newTestProxyRepo` helper 并不存在**,派发时必须纠正
- service 包全量测试约 145s,含已知偶发失败 `TestContentModerationRuntimeSnapshotRefreshFailureKeepsStaleConfig`(上游同样存在,可忽略)
- 管道接 tail/grep 会掩盖 go test 的真实退出码,判断成败要看输出里的 ok/FAIL 行

## Task 5 实现要点(spike 已定,obfs 必需)
- 测试节点参数见会话/派发提示词,**不写进本文件**(含密码)
- obfs-host 形如 `xxx.default.microsoft.fi:249057` —— 末尾冒号数字**不是端口**,
  必须整串原样作为 SNI 传入,**不得 split**
- 服务端不主动回 ServerHello,故"发探针看响应"拿不到 skip 字节数;
  必须实现完整 ClientHello(载荷嵌入)后端到端迭代,以 HTTP 401 为通过标志
- 已备好探针程序 backend/tmp_obfs/(可改造复用),tmp_spike/ 是纯 ss 版
- 三个临时目录 tmp_spike/ tmp_obfs/ tmp_obfs_e2e/ 在 Task 5 完成后统一删除

## 待办/风险
- Task 4/5 现在可派发(obfs 分支必须保留)
- go-shadowsocks2 v0.1.5 已加入 go.mod(当前标记 indirect,待 Task 4 产品代码 import 后 tidy 会转正)
- 两个 subagent 曾卡在"启动后台命令后停下等通知";派发时要求**前台执行**

---
# 历史 — 2026-07-16 批量测试/置错/CPA导入 计划(docs/superpowers/plans/2026-07-16-grok-batch-test-cpa-import.md)
分支: develop/xuyang/batch-test-cpa-import  基点: 93f904cd

## 本计划任务
- Task 1: complete (commits 93f904cd..2f5133d4, review clean)
- Task 2: complete (commits 2f5133d4..96254a0f, review clean)
- Task 3: complete (commits 96254a0f..92dee164, review clean)
- Task 4: complete (commits 92dee164..7b40532f, review clean)
- Task 5: complete (commits 7b40532f..e928dc31, review clean)
- Task 6: complete (commits e928dc31..fa0b3b1f, review clean)
- Task 7-9: complete (commits fa0b3b1f..8e763bb4, review clean)
- Task 10: complete (commits 8e763bb4..f0728641, CUSTOM_CHANGES 登记)
- Final: HEAD f0728641, 分支 develop/xuyang/batch-test-cpa-import 已推 origin
---
## 历史(2026-07 发版计划,已完结)
Task 1: complete (commits 9a7f9662..e015a533, review clean)
  Minor(留给终审): parseVersion 对多'-'后缀/负数 N 静默兜底为0,理论边界,与上游风格一致
Task 2: complete (commits e015a533..f47c5e50, review clean)
Task 3: complete (commits f47c5e50..dda560a6, review clean)
  Minor(留给终审): format: tar.gz 在 goreleaser v2.6+ 有弃用警告,与上游配置一致保留
Task 4: complete (push dda560a6, 旧标签 custom-v0.1.156 已删本地+远程)
Task 5: complete (custom/v0.1.155-maint = 2be10837 + cherry-pick 41c8e4bf/5181da22/194c0254, 测试 ok, 已推送)
Task 7: complete (v0.1.155-custom.1 发布成功,资产齐全,prerelease=false)
Task 8: complete (v0.1.156-custom.1 发布成功,latest 正确,ghcr 三标签均 200)
Task final-review: READY (修复 406f5fb6/6f636205 + install.sh 86ce7591)
Final: READY(终审复核闭环) + install.sh 修复(86ce7591)
发布: v0.1.155-custom.2 / v0.1.156-custom.2 全部成功,latest(release+docker)均指向 0.1.156-custom.2
遗留: CI golangci-lint 失败(已挂 spawn_task 后续处理);custom.1 两个 release 保留(镜像/资产完整,仅前端常量指旧仓库)

## 终审结论(2026-07-27) — 修完 Critical/Important 后可合并
**Critical(逐任务审查全部漏掉,全分支视角才发现)**：
`proxyurl.Parse` 放行 ss 后,仓库有 4 类代理消费者,本分支只改造了 2 类。
未覆盖的 req/v3 客户端池(Claude/Grok/OpenAI/Gemini OAuth token 刷新)会把 ss:// 当
普通 HTTP 代理,向 ss 节点端口发明文 CONNECT → 刷新失败 → 叠加定制的
「刷新失败→SetError」逻辑 → **账号被自动禁用**。恰好发生在本功能目标场景里。

已全部修复(4 个提交 52e4706ad/045a1f796/27a2c3dc2/1c36f0df6):
- C1 req/v3 + I1 WS桥：新增 applyReqClientProxy 统一分派;ss 走 SetDial
  **且必须 SetProxy(nil)** —— req.T() 默认 Proxy=ProxyFromEnvironment,
  只调 SetDial 时宿主机有 HTTP_PROXY 会用 ss 隧道去连环境代理(修复方发现,终审也漏了)
- I2 dto 补 Extra 映射(此前接口从不返回 extra,前端 TS 字段永远 undefined)
- I3 删前端 protocol!=='ss' 规避(否则把已有代理改成 ss 会静默失败还提示成功)
- I4 CUSTOM_CHANGES 保护清单补全 + I5 守卫单测(做了变异测试证明有牙齿) + I6 atlas 耦合记录

## 未做/待办
- 终审的 6 条 Minor 未处理(M1 源码写死真实节点域名建议脱敏;M2 yaml 错误回显;
  M3 订阅拉取无内网过滤;M4 密码轮换后重复创建而非更新;M5 dry_run 不批内去重;
  M6 UI 手工建 ss 只能建出无 obfs 节点、无提示)
- **真实链路验收需用户的远端部署实例**(管理台导入订阅→绑账号→发真实请求)
- 分支未 push,未合 main

## 2026-07-28 实机验证与关键修复
**真实环境验证已通过**(docker s2a-test, 127.0.0.1:8900, admin@s2a.local):
- 订阅导入成功: 40 个 ss 节点入库,protocol=ss, username=cipher,
  extra={"mode":"tls","plugin":"obfs","obfs-host":"..."} 全部正确
- 迁移增量升级验证: 旧库停在 176_,新二进制补跑 177_~185_ + 9001_,共 219→230
- 账号绑 ss 代理测试连接: **3.5 秒返回 grok 真实回复**(修复后)

### 修复的 Critical bug (commit 29eaf0a3f)
`repository/http_upstream.go` 的 `normalizeProxyURL` 为算连接池缓存键清空 RawQuery,
而返回的同一个 parsed 又用于建 transport → ss 的 obfs 参数被抹掉 → 裸 ss 连接 →
服务端静默丢弃 → `Post ...: EOF`。
**症状极隐蔽**:代理延迟探测、订阅导入、全部单测、TLS 指纹路径都正常,
只有 httpUpstream.Do 路径(真实转发+账号测试)失败。
修复:删除该行,query 一并进缓存键(顺带解决 obfs 参数不同的节点共用连接池)。
补 3 个回归测试并做过变异测试。已置顶登记进 CUSTOM_CHANGES 必查清单。

### 排查过程的教训(重要)
先后误判三次:TLS 指纹 → 节点限流 → DNS 解析,都是**基于旁证下结论**。
真正定位靠的是在 ss dialer 里加临时日志打出 `obfs=""`。
**教训:跨层假设(「参数走 query,下游无感」)必须逐个验证下游,不能假设。**
另:压测把两个机场节点打限流了,制造了额外噪音,排查时应轮换节点。

## 待办
- **代理列表按延迟/质量排序**(用户原始需求「一眼看出哪个节点延迟最低」,未做)
  现状:延迟已展示但 `sortable: false`;数据在 Redis 不在库,需内存排序。
  已确认前端有 `fetchAllProxiesForBatch()` 可复用,几十个节点纯前端排序即可。
  建议口径:质量当门槛(筛掉 D/E)+ 延迟当排序键。
- 全选所有数据:用户已决定**不做**(批量质检/连接测试不勾选即全量,够用)
- 终审 6 条 Minor 仍未处理
- 分支未 push,未合 main

## 2026-07-28 追加：代理选择器延迟展示与排序 (commit 897d9a301)
账号编辑弹窗的代理下拉此前只显示「本次在下拉里点过测试」的结果,大部分空白且无排序。
- `ProxySelector.vue`: 无本次测试结果时回落到持久化 latency_ms + country;
  默认按延迟升序,**无数据/探测失败排最后**(避免"从未探测"被误当成"很快")
- `AccountsView.vue`: `proxies.getAll()` → `getAllWithCount()`
  **根因**: api/admin/proxies.ts 有两个函数,`getAll()` 走 `/admin/proxies/all`(不带 with_count),
  后端返回裸 Proxy 不含 latency_ms/quality_*/country;`getAllWithCount()` 才带。
  账号页用错了函数,所以选择器永远拿不到延迟。
实测: 147ms/159ms/162ms/164ms 升序,国家正确。vue-tsc 无错误,vitest 1244 全绿。
注: SettingsView 用 `proxies.list()` → `ListProxiesWithAccountCount`,本来就带延迟,无需改。

## 订阅协议支持边界(用户实测遇到)
用户提供的另一个订阅 `apidc.dnso.ccwu.cc:2096/clash/...` 导入不了:
实测该订阅 **15 个节点全是 type: hysteria2**,0 个 ss。
按设计非目标,解析器会全部跳过并给出「不支持的节点类型 hysteria2(仅支持 ss)」。
**不是 bug**。若要支持 hysteria2:它跑在 QUIC/UDP 上,需引入 quic-go 全套协议栈 +
自有认证/拥塞控制,实现量与依赖体量比 ss+obfs 高一个数量级,需重新过许可证审查。
