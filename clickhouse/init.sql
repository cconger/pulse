CREATE DATABASE IF NOT EXISTS pulse;

CREATE TABLE IF NOT EXISTS pulse.checkin
  (
    channel String,
    source String,
    target_user String,
    target_topic String,
    value Int8,
    timestamp DateTime
  )
  Engine = MergeTree()
  PARTITION BY toYYYYMM(timestamp)
  ORDER BY (channel, timestamp)
  SETTINGS index_granularity = 8192;
