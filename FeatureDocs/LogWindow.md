**LogWindow Feature**

The web interface will have a log window that shows each sync event after it happens
It will use a ring buffer to show the last 10 log entries

Entries in the log should show:
- Description of event type showing if it is either syncing from or to this device
- Should have a timestamp
- Should show the size of the file
- Should show the local filename for the file
- Should show the name of the group that the file belongs to
