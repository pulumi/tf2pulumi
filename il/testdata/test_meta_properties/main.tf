resource "aws_instance" "r1" {
  timeouts {
    "create" = "20m"
    "update" = "5m"
    "delete" = "1h"
  }
}

resource "aws_instance" "r2" {
  lifecycle {
    ignore_changes = [ "*" ]
  }
}

resource "aws_instance" "r3" {
  lifecycle {
    ignore_changes = [ "ami", "user_data", "tags.Creator", "network_interface.0.network_interface_id", "root_block_device.0.encrypted" ]
  }
}
