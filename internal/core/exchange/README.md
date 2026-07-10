# exchange

`exchange` 定义代理交换记录、采集配置以及出站端口，不包含 HTTP、JSON、Huma 或消息中间件实现。

- `Collector` 是采集 adapter 提交完整记录的入口，由 `service.ExchangeService` 实现。
- `Writer` 是外部介质的出站端口。Kafka 等 adapter 只实现该端口，并自行负责序列化、缓冲、重试和关闭。
- 内存保留、查询和 writer 编排属于 service；HTTP body 包装和敏感 header 脱敏属于 capture adapter。
