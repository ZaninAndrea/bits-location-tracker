# Bits Location Tracker

This is the backend for a location tracker connected with the bits platform, data is provided to the server by the [GPSLogger app](https://gpslogger.app/).

The GPS location is matched against known places and the recognized place is tracked in the bits database, the precise GPS location is discarded.

## Configuration

The server expects to be provided `MONGO_URI` and `PORT` either in a `.env` file or directly as environment variables.
