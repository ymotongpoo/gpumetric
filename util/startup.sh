#!/bin/bash

USER_HOME=/home/${user}
PROJECT_ROOT=$USER_HOME/gpumetric
COLLECTOR_ROOT=$USER_HOME/opentelemetry-operations-collector
COLLECTOR_BIN=google-cloud-metrics-agent_linux_amd64
COLLECTOR_PATH=/usr/local/bin/$COLLECTOR_BIN
METRIC_BIN=gpumetric
METRIC_PATH=/usr/local/bin/$METRIC_BIN
OTEL_CONFIG=$USER_HOME/otel-config.yaml

/opt/deeplearning/install-driver.sh
apt-get install -y libnvidia-container-dev libnvidia-container1
wget -c https://storage.googleapis.com/golang/go1.15.6.linux-amd64.tar.gz -O - | tar xz -C /opt
echo export PATH=/opt/go/bin:\$PATH >> /etc/profile.d/golang.sh
chmod +x /etc/profile.d/golang.sh
export PATH=/opt/go/bin:$PATH
export GOCACHE=/tmp
export GOPATH=/home/${user}/go

cd $USER_HOME
su - ${user} -c "git clone -b issue/2 https://github.com/ymotongpoo/gpumetric.git"
chown -R ${user}:${user} $PROJECT_ROOT
cd $PROJECT_ROOT
go build -o $METRIC_BIN >$USER_HOME/metric-build.log 2>&1
mv $METRIC_BIN $METRIC_PATH

cd $USER_HOME
su - ${user} -c "git clone -b update https://github.com/ymotongpoo/opentelemetry-operations-collector.git"
chown -R ${user}:${user} $COLLECTOR_ROOT
cd $COLLECTOR_ROOT
make build >$USER_HOME/collector-build.log 2>&1
mv $COLLECTOR_ROOT/bin/$COLLECTOR_BIN $COLLECTOR_PATH

cat <<EOF > /home/${user}/otel-config.yaml
# The original version is here:
# https://github.com/GoogleCloudPlatform/opentelemetry-operations-collector/blob/master/config/config-example.yaml

receivers:
  hostmetrics:
    collection_interval: 1m
    scrapers:
      cpu:
      filesystem:
      load:
      memory:
      network:
      paging:
  otlp:
    protocols:
      http:

processors:
  # Detect GCE info
  resourcedetection:
    detectors: [gce]

  # perform custom transformations that aren't supported by the metricstransform processor
  agentmetrics/host:
    # 1. combines resource process metrics into metrics with processes as labels
    # 2. splits "disk.io" metrics into read & write metrics
    # 3. creates utilization metrics from usage metrics

  filter/host:
    metrics:
      exclude:
        match_type: strict
        metric_names:
          - system.network.dropped_packets
          - system.filesystem.inodes.usage
          - system.paging.faults

  metricstransform/host:
    transforms:
      # system.cpu.time -> cpu/usage_time
      - metric_name: system.cpu.time
        action: update
        new_name: agent.googleapis.com/cpu/usage_time
        operations:
          # change data type from double -> int64
          - action: toggle_scalar_data_type
          # change label cpu -> cpu_number
          - action: update_label
            label: cpu
            new_label: cpu_number
          # change label state -> cpu_state
          - action: update_label
            label: state
            new_label: cpu_state
          # take mean over cpu_number dimension, retaining only cpu_state
          - action: aggregate_labels
            label_set: [ cpu_state ]
            aggregation_type: mean
      # system.cpu.utilization -> cpu/utilization
      - metric_name: system.cpu.utilization
        action: update
        new_name: agent.googleapis.com/cpu/utilization
        operations:
          # change label cpu -> cpu_number
          - action: update_label
            label: cpu
            new_label: cpu_number
          # change label state -> cpu_state
          - action: update_label
            label: state
            new_label: cpu_state
          # take mean over cpu_number dimension, retaining only cpu_state
          - action: aggregate_labels
            label_set: [ cpu_state ]
            aggregation_type: mean
      # system.cpu.load_average.1m -> cpu/load_1m
      - metric_name: system.cpu.load_average.1m
        action: update
        new_name: agent.googleapis.com/cpu/load_1m
      # system.cpu.load_average.5m -> cpu/load_5m
      - metric_name: system.cpu.load_average.5m
        action: update
        new_name: agent.googleapis.com/cpu/load_5m
      # system.cpu.load_average.15m -> cpu/load_15m
      - metric_name: system.cpu.load_average.15m
        action: update
        new_name: agent.googleapis.com/cpu/load_15m
      # system.disk.read_io (as named after custom split logic) -> disk/read_bytes_count
      - metric_name: system.disk.read_io
        action: update
        new_name: agent.googleapis.com/disk/read_bytes_count
      # system.disk.write_io (as named after custom split logic) -> processes/write_bytes_count
      - metric_name: system.disk.write_io
        action: update
        new_name: agent.googleapis.com/disk/write_bytes_count
      # system.disk.ops -> disk/operation_count
      - metric_name: system.disk.ops
        action: update
        new_name: agent.googleapis.com/disk/operation_count
      # system.disk.operation_time -> disk/operation_time
      - metric_name: system.disk.operation_time
        action: update
        new_name: agent.googleapis.com/disk/operation_time
        operations:
          # change data type from double -> int64
          - action: toggle_scalar_data_type
      # system.disk.pending_operations -> disk/pending_operations
      - metric_name: system.disk.pending_operations
        action: update
        new_name: agent.googleapis.com/disk/pending_operations
      # system.disk.merged -> disk/merged_operations
      - metric_name: system.disk.merged
        action: update
        new_name: agent.googleapis.com/disk/merged_operations
      # system.filesystem.usage -> disk/bytes_used
      - metric_name: system.filesystem.usage
        action: update
        new_name: agent.googleapis.com/disk/bytes_used
        operations:
          # change data type from int64 -> double
          - action: toggle_scalar_data_type
          # take sum over mode, mountpoint & type dimensions, retaining only device & state
          - action: aggregate_labels
            label_set: [ device, state ]
            aggregation_type: sum
      # system.filesystem.utilization -> disk/percent_used
      - metric_name: system.filesystem.utilization
        action: update
        new_name: agent.googleapis.com/disk/percent_used
        operations:
          # take sum over mode, mountpoint & type dimensions, retaining only device & state
          - action: aggregate_labels
            label_set: [ device, state ]
            aggregation_type: sum
      # system.memory.usage -> memory/bytes_used
      - metric_name: system.memory.usage
        action: update
        new_name: agent.googleapis.com/memory/bytes_used
        operations:
          # change data type from int64 -> double
          - action: toggle_scalar_data_type
          # aggregate state label values: slab_reclaimable & slab_unreclaimable -> slab (note this is not currently supported)
          - action: aggregate_label_values
            label: state
            aggregated_values: [slab_reclaimable, slab_unreclaimable]
            new_value: slab
            aggregation_type: sum
      # system.memory.utilization -> memory/percent_used
      - metric_name: system.memory.utilization
        action: update
        new_name: agent.googleapis.com/memory/percent_used
        operations:
          # sum state label values: slab = slab_reclaimable + slab_unreclaimable
          - action: aggregate_label_values
            label: state
            aggregated_values: [slab_reclaimable, slab_unreclaimable]
            new_value: slab
            aggregation_type: sum
      # system.network.io -> interface/traffic
      - metric_name: system.network.io
        action: update
        new_name: agent.googleapis.com/interface/traffic
        operations:
          # change direction label values receive -> rx
          - action: update_label
            label: direction
            value_actions:
              # receive -> rx
              - value: receive
                new_value: rx
              # transmit -> tx
              - value: transmit
                new_value: tx
      # system.network.errors -> interface/errors
      - metric_name: system.network.errors
        action: update
        new_name: agent.googleapis.com/interface/errors
        operations:
          # change direction label values receive -> rx
          - action: update_label
            label: direction
            value_actions:
              # receive -> rx
              - value: receive
                new_value: rx
              # transmit -> tx
              - value: transmit
                new_value: tx
      # system.network.packets -> interface/packets
      - metric_name: system.network.packets
        action: update
        new_name: agent.googleapis.com/interface/packets
        operations:
          # change direction label values receive -> rx
          - action: update_label
            label: direction
            value_actions:
              # receive -> rx
              - value: receive
                new_value: rx
              # transmit -> tx
              - value: transmit
                new_value: tx
      # system.network.tcp_connections -> network/tcp_connections
      - metric_name: system.network.tcp_connections
        action: update
        new_name: agent.googleapis.com/network/tcp_connections
        operations:
          # change data type from int64 -> double
          - action: toggle_scalar_data_type
          # change label state -> tcp_state
          - action: update_label
            label: state
            new_label: tcp_state
      # system.paging.usage -> swap/bytes_used
      - metric_name: system.paging.usage
        action: update
        new_name: agent.googleapis.com/swap/bytes_used
        operations:
          # change data type from int64 -> double
          - action: toggle_scalar_data_type
      # system.paging.utilization -> swap/percent_used
      - metric_name: system.paging.utilization
        action: update
        new_name: agent.googleapis.com/swap/percent_used
        operations:
          # take sum over direction dimension, retaining only state
          - action: aggregate_labels
            label_set: [ direction ]
            aggregation_type: sum
      # process.cpu.time -> processes/cpu_time
      - metric_name: process.cpu.time
        action: update
        new_name: agent.googleapis.com/processes/cpu_time
        operations:
          # change data type from double -> int64
          - action: toggle_scalar_data_type
          # change label state -> user_or_syst
          - action: update_label
            label: state
            new_label: user_or_syst
      # process.disk.read_io (as named after custom split logic) -> processes/disk/read_bytes_count
      - metric_name: process.disk.read_io
        action: update
        new_name: agent.googleapis.com/processes/disk/read_bytes_count
      # process.disk.write_io (as named after custom split logic) -> processes/disk/write_bytes_count
      - metric_name: process.disk.write_io
        action: update
        new_name: agent.googleapis.com/processes/disk/write_bytes_count
      # process.memory.physical_usage -> processes/rss_usage
      - metric_name: process.memory.physical_usage
        action: update
        new_name: agent.googleapis.com/processes/rss_usage
        operations:
          # change data type from int64 -> double
          - action: toggle_scalar_data_type
      # process.memory.virtual_usage -> processes/vm_usage
      - metric_name: process.memory.virtual_usage
        action: update
        new_name: agent.googleapis.com/processes/vm_usage
        operations:
          # change data type from int64 -> double
          - action: toggle_scalar_data_type

exporters:
  # this exporter will prefix all metrics without a domain (eg. agent.googleapis.com/) with custom.googleapis.com/
  # also, it will use the default GCE instance credentials this Collector will run on
  stackdriver:
    metric:
      prefix: custom.googleapis.com/
  logging:
    loglevel: debug

service:
  pipelines:
    metrics:
      receivers: [hostmetrics, otlp]
      processors: [agentmetrics/host, metricstransform/host, filter/host, resourcedetection]
      exporters: [stackdriver, logging]
EOF

cd $USER_HOME
nohup $COLLECTOR_PATH --config $OTEL_CONFIG &
nohup $METRIC_PATH &

touch done
metric_id=$(ps aux | grep gpumetric | grep -v grep | tr -s ' ' ' ' | cut -d' ' -f2)
collector_id=$(ps aux | grep google-cloud | grep -v grep | tr -s ' ' ' ' | cut -d' ' -f2)
echo -e "gpumetric\t$metric_id\ncollector\t$collector_id" >> done