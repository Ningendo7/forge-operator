output "node_security_group_id" {

         description = "The ID of the security group attached to the Forge cluster nodes"
         value       = aws_security_group.forge-nodes-sg.id
  
}