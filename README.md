# SimFaaS

SimFaaS is aimed to be a generic FaaS emulator/simulator (though currently it 
is just an emulator). The goal is to be able to evaluate key metrics, such as 
runtime, cold start duration and resource cost, in FaaS platforms.

## Building

See Makefile.

## Fission

SimFaaS includes a specific implementation for Fission called: **SimFission**.
SimFission is a wrapper on top of simfaas that emulates a part of the
API of Fission.

It currently emulates the following part of the (web) API:
1. Resolving a function name to a service.
2. Tapping of a service.
3. Executing a fission function.
4. Getting info for a given function.