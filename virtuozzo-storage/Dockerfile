FROM centos:7
ENTRYPOINT ["virtuozzo-storage"]
CMD -master=http://127.0.0.1:8080 -kubeconfig=/kube/config

ADD virtuozzo.repo /etc/yum.repos.d/
RUN printf "upgrade \n install ploop gdisk \n run" | yum shell -y

ADD virtuozzo-storage /usr/local/bin/virtuozzo-storage
RUN chmod x+o /usr/local/bin/virtuozzo-storage
