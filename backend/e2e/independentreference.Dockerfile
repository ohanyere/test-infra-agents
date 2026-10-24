FROM scratch
COPY referenceapp /referenceapp
ENTRYPOINT ["/referenceapp"]
