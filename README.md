# Gaslight

> A mock EVM RPC that lies to your indexer, so you can find out how it breaks before mainnet does.

Gaslight is a mock Ethereum JSON-RPC node that **lies** — it injects real-world
chain faults (reorgs, stale/frozen nodes, false-`200 OK` responses) driven by a
declarative scenario file, so you can test whether your Web3 backend survives a
misbehaving node — **reproducibly, in CI**.

Point your indexer / deposit watcher / RPC gateway at Gaslight instead of a real
node, run a scenario, and watch how it copes when the chain reorgs a confirmed
deposit out of existence.

> [!NOTE]
> Early development. Tracking issue: [#11](https://github.com/ChickenBenny/Gaslight/issues/11).
> This README is a stub — full quickstart and scenario docs land with v0.1.

## Why not just use Anvil / Toxiproxy?

- **Anvil / Hardhat** are *well-behaved* nodes — they don't lie to you.
- **Toxiproxy / Chaos Mesh** understand *networks*, not *chains* — they can't
  reorg a block or return an orphaned log.

Gaslight fills the gap: **chain-semantic, application-layer fault injection,
built for reproducible CI tests.**

## Development

```sh
make ci    # gofmt check + vet + build + test (mirrors GitHub Actions)
make run   # print version
```

## License

See [LICENSE](LICENSE).
