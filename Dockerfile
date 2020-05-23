FROM scratch
COPY dispatchers /app
ENTRYPOINT ["/app"]

