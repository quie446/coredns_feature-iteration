# cidrttl

## Name

*cidrttl* - rewrites the TTL of positive DNS answers based on the client source
network.

## Description

The *cidrttl* plugin maps client source networks (CIDRs) to a TTL value. When a
positive response with answers is sent to a client whose source address belongs
to a configured network, the TTL of every answer record is set to the mapped
value. It hooks into the existing response chain; it does not run a separate
resolver or alter record data.

Rules:

* Only positive (`NOERROR`) responses containing answers are rewritten.
* Negative answers and empty answers are forwarded untouched, so a name without
  records never gains any.
* Sources not covered by any network are forwarded untouched.
* Overlapping networks resolve to the most specific (longest-prefix) match,
  regardless of file ordering. Lookup order is deterministic on every machine.

## Syntax

```
cidrttl [FILE] {
    reload [DURATION]
}
```

* **FILE** is the path to the mapping table (required).
* `reload` sets the polling interval; defaults to `30s`. `0` disables hot
  reload.

## Mapping Table

One entry per non-empty line, two whitespace-separated fields:

```
CIDR TTL
```

* `CIDR` must be legal network notation with no host bits set, e.g.
  `10.0.0.0/8` or `2001:db8::/32`.
* `TTL` must be a non-negative integer. Blank, negative or non-numeric values
  reject the whole table.
* Lines starting with `#` are comments.

Validation reports the first offending line number. A table that fails to load
(missing file, invalid row, or an empty table) is never activated: at startup
the server refuses to start, and on hot reload the previously active table
keeps serving while the failure reason is logged and counted.

## Examples

```
. {
    cidrttl /etc/coredns/cidr-ttl.table {
        reload 10s
    }
    forward . 1.1.1.1
}
```

```
# /etc/coredns/cidr-ttl.table
10.0.0.0/8       60
10.1.0.0/16      30
2001:db8::/32    120
```

## Metrics

* `coredns_cidrttl_ttl_rewrite_hits_total{server,view,network,source,old_ttl,new_ttl}` -
  answer records rewritten, labeled with the matched network, client source,
  and old/new TTL.
* `coredns_cidrttl_ttl_rewrite_misses_total{server,view}` - positive answers
  from unmatched sources.
* `coredns_cidrttl_ttl_rewrite_failures_total{server,view}` - responses aborted
  due to a rewrite failure.
* `coredns_cidrttl_table_reload_success_total{server,view}` - successful table
  reloads.
* `coredns_cidrttl_table_reload_failure_total{server,view,reason}` - failed
  reloads with the failure reason.
