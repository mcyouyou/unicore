# docker build -t unicore-daemon .
# kind load docker-image unicore-daemon -n dev
FROM alpine
COPY ./daemon /usr/local/bin/daemon
RUN chmod +x /usr/local/bin/daemon
CMD ["daemon"]