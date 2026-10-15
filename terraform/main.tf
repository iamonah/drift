variable "access_key" {
  type = string
}
variable "secret_key" {
  type = string
}
variable "region" {
  type = string
}
variable "s3_endpoint" {
  type = string
}
variable "sqs_endpoint" {
  type = string
}

variable "bucket_name" {
  type = string
}

provider "aws" {
  access_key = var.access_key
  secret_key = var.secret_key
  region     = var.region
 
  s3_use_path_style         = true
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true

  endpoints {
    s3 = var.s3_endpoint
    sqs = var.sqs_endpoint
  }
}

resource "aws_s3_bucket" "test_bucket" {
  bucket = var.bucket_name
}

variable "queue_name" {
  type = string
}

resource "aws_sqs_queue" "terraform_queue" {
  name                      = var.queue_name
  delay_seconds             = 90
  max_message_size          = 2048
  message_retention_seconds = 86400
  receive_wait_time_seconds = 10
}
