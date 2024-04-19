# gosip
This package implements S/IP, the Sercos Internet Protocol for accessing data in Sercos devices, in go.

Supported services:
- TCP ReadEverything, ReadDescription, ReadOnlyData, ReadDataState for reading parameter data 
- TCP WriteData for writing parameter data
- UDP Browse to browse for devices supporting the S/IP protocol.
