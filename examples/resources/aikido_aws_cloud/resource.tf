# Single AWS account. Create the IAM role in AWS first (see Aikido AWS cloud docs).
resource "aikido_aws_cloud" "production" {
  name        = "AWS production"
  role_arn    = "arn:aws:iam::000000000000:role/aikido-security-readonly"
  environment = "production"
}

# Full AWS Organization via the management account role.
resource "aikido_aws_cloud" "organization" {
  name        = "AWS organization"
  role_arn    = "arn:aws:iam::111111111111:role/aikido-security-readonly"
  environment = "mixed"

  organization = {
    organization_id        = "o-1234567890"
    synced_unit_ids        = ["r-abcd"]
    excluded_accounts      = ["222222222222"]
    container_scan_enabled = true
    vm_scan_enabled        = false
  }
}
