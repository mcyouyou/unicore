# Unicore
K8s based CD & operation toolset developed by the mcyouyou dev team.

## Deployer
The Deployer service is responsible for deploying business containers according to specifications and maintaining metadata of business services. 
It also provides monitoring capabilities for business containers, including status, events, logs, and more.

## CD
The CD service offers gray-release, rollback, scaling, and migration capabilities. Additionally, it includes functions for staging environment management and version control.

## Infra
Infra encompasses multiple services, such as traffic control, service discovery, image repository, cache service and storage orchestration.

## Automation
Automation consists of multiple functions that handle automated operations, including HPA (Horizontal Pod Autoscaling),
auto-balancing, and auto-certificate management.

## Control Panel
Backend for unicore control, 