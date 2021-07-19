## 蓝鲸日志采集器

蓝鲸日志采集器基于GSE采集框架2.0 & Filebeat进行开发，并为日志平台、计算平台、BCS等平台提供日志采集服务。 

```
 ./                                # 日志采集器
 |-- beater                        # 采集器进程管理、任务管理、日志事件Acker处理       
 |-- config                        # 配置管理模块      
 |-- include                       # Filebeat插件及配置加载       
 |-- registrar                     # 采集进度模块      
 |-- support-files                 # 部署模板
 |-- task                          # 采集任务实现（日志事件处理、过滤、打包、发送） 
 |-- main.go                       # 采集器入口
```