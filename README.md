# Unicore
[![可维护性评级](https://sonarqube.pysio.online/api/project_badges/measure?project=mcyouyou_unicore_7084492c-5755-412c-b8a0-6b032f386028&metric=sqale_rating&token=sqb_b19a2a8bf51d0854eea72a12e229f94c96657bf8)](https://sonarqube.pysio.online/dashboard?id=mcyouyou_unicore_7084492c-5755-412c-b8a0-6b032f386028)
[![安全等级](https://sonarqube.pysio.online/api/project_badges/measure?project=mcyouyou_unicore_7084492c-5755-412c-b8a0-6b032f386028&metric=security_rating&token=sqb_b19a2a8bf51d0854eea72a12e229f94c96657bf8)](https://sonarqube.pysio.online/dashboard?id=mcyouyou_unicore_7084492c-5755-412c-b8a0-6b032f386028)
[![代码行数](https://sonarqube.pysio.online/api/project_badges/measure?project=mcyouyou_unicore_7084492c-5755-412c-b8a0-6b032f386028&metric=ncloc&token=sqb_b19a2a8bf51d0854eea72a12e229f94c96657bf8)](https://sonarqube.pysio.online/dashboard?id=mcyouyou_unicore_7084492c-5755-412c-b8a0-6b032f386028)
[![覆盖率](https://sonarqube.pysio.online/api/project_badges/measure?project=mcyouyou_unicore_7084492c-5755-412c-b8a0-6b032f386028&metric=coverage&token=sqb_b19a2a8bf51d0854eea72a12e229f94c96657bf8)](https://sonarqube.pysio.online/dashboard?id=mcyouyou_unicore_7084492c-5755-412c-b8a0-6b032f386028)
[![警报](https://sonarqube.pysio.online/api/project_badges/measure?project=mcyouyou_unicore_7084492c-5755-412c-b8a0-6b032f386028&metric=alert_status&token=sqb_b19a2a8bf51d0854eea72a12e229f94c96657bf8)](https://sonarqube.pysio.online/dashboard?id=mcyouyou_unicore_7084492c-5755-412c-b8a0-6b032f386028)

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