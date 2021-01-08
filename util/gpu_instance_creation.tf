# Copyright 2021 Yoshi Yamaguchi
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

variable "project_id" {
    type = string
    description = "Google Cloud Platform project ID"
}

variable "instance_name" {
    type = string
    description = "Google Compute Engine instance name"
}

variable "region" {
    type = string
    description = "Google Compute Engine region"
    default = "asia-east1"
}

variable "zone" {
    type = string
    description = "Google Compute Engine zone"
    default = "asia-east1-b"
}

variable "user" {
    type = string
    description = "username for SSH"
}

provider "google" {
    project = var.project_id
    region = var.region
}

resource "tls_private_key" "ssh-key" {
    algorithm = "RSA"
    rsa_bits = 4096
}

resource "google_compute_firewall" "gpu-instance-ssh" {
    name = "gpu-instance-ssh"
    network = "default"

    allow {
        protocol = "tcp"
        ports = ["22"]
    }

    source_ranges = ["0.0.0.0/0"]
    target_tags = ["gpu-test-instances"]
}

resource "google_compute_instance" "gpu_instance_creation" {
    name = var.instance_name
    machine_type = "n1-standard-8"
    zone = var.zone
    tags = ["gpu-test-instance"]

    guest_accelerator {
        type = "nvidia-tesla-k80"
        count = 1
    }

    boot_disk {
        initialize_params {
            type = "pd-standard"
            image = "deeplearning-platform-release/common-cu110"
        }
    }

    network_interface {
        network = "default"

        access_config {}
    }

    scheduling {
        automatic_restart = true
        on_host_maintenance = "TERMINATE"
    }

    metadata = {
        install-nvidia-driver = "True"
        sshKeys = "${var.user}:${tls_private_key.ssh-key.public_key_openssh}"
    }

    metadata_startup_script = <<EOT
/opt/deeplearning/install-driver.sh
apt-get install -y libnvidia-container-dev libnvidia-container1
wget -c https://storage.googleapis.com/golang/go1.15.6.linux-amd64.tar.gz -O - | tar xz -C /opt
echo export PATH=/opt/go/bin:\$PATH >> /etc/profile.d/golang.sh
chmod +x /etc/profile.d/golang.sh
su - demo && cd /home/demo && git clone https://github.com/ymotongpoo/gpumetric.git
chwon -R demo:demo /home/demo/gpumetric
EOT

    depends_on = ["google_compute_firewall.gpu-instance-ssh"]

    service_account {
        scopes = ["logging-write", "monitoring-write"]
    }
}