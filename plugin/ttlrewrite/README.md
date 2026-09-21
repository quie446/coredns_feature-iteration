# ttlrewrite

## Name

*ttlrewrite* - rewrites the TTL of positive DNS answers based on the client's
source network, using a longest-prefix-match subnet table.

## Description

The *ttlrewrite* plugin sits in the normal plugin chain and adjusts the TTL of
records in the answer section of positive (NOERROR) responses. It never
resolves queries itself and never changes the content of answers: only the TTL
of answer records is rewritten. Negative responses (NXDOMAIN, NODATA, errors)
and requests from clients that match no rule are passed through untouched.

The subnet table is loaded from a file at startup and hot-reloaded whenever
the file changes, so an updated table takes effect without restarting the
process. A failed reload keeps the previous table and logs the exact reason.

## Syntax

```
ttlrewrite FILE {
    reload DURATION
}
```

* **FILE** is the subnet table file. Each line contains `<cidr> <ttl>`:

  ```
  # subnet            ttl (seconds)
  10.0.0.0/8          30
  10.1.0.0/16         60
  2001:db8::/32       300
  ```

  Empty lines and lines starting with `#` are ignored. The table is validated
  on load: a missing field, an invalid network, or a negative/blank TTL aborts
  the load and reports the offending line. Overlapping networks are matched by
  longest prefix first.

* `reload` sets how often the table file is checked for changes (default `1s`).

## Metrics

Every rewrite is logged with the client address, query name, matched network,
and the old and new TTL values.

## Examples

```
. {
    ttlrewrite /etc/coredns/ttl-rules.txt {
        reload 5s
    }
    forward . 8.8.8.8
}
```

Clients in `10.0.0.0/8` receive answers with the TTL from the table; all other
clients receive the upstream TTL unchanged.
