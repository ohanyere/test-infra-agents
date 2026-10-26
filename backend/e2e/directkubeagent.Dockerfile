FROM scratch
COPY directkubeagent /directkubeagent
ENTRYPOINT ["/directkubeagent"]
